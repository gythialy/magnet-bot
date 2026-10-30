package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"time"

	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"

	"github.com/glebarez/sqlite"
	"github.com/go-co-op/gocron"
	"github.com/go-telegram/bot"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type dbLogger struct {
	*utils.Logger
}

func (dl *dbLogger) LogMode(_ logger.LogLevel) logger.Interface {
	return dl
}

func (dl *dbLogger) Info(_ context.Context, msg string, data ...any) {
	dl.Logger.Info().Msgf(msg, data...)
}

func (dl *dbLogger) Warn(_ context.Context, msg string, data ...any) {
	dl.Logger.Warn().Msgf(msg, data...)
}

func (dl *dbLogger) Error(_ context.Context, msg string, data ...any) {
	dl.Logger.Error().Msgf(msg, data...)
}

func (dl *dbLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	if err != nil {
		dl.Logger.Error().Msgf("[%.3fms] [rows:%v] %s; %s", float64(elapsed.Nanoseconds())/1e6, rows, sql, err)
	} else {
		dl.Logger.Info().Msgf("[%.3fms] [rows:%v] %s", float64(elapsed.Nanoseconds())/1e6, rows, sql)
	}
}

type BotContext struct {
	Bot             *bot.Bot
	Store           *Store[model.RequestInfo]
	Logger          *utils.Logger
	Config          *config.ServiceConfig
	Gotenberg       *GotenbergClient
	ctx             context.Context
	cancel          context.CancelFunc
	scheduler       *gocron.Scheduler
	processor       *InfoProcessor
	cmdHandler      *CommandsHandler
	shutdownWebhook func()
}

// context returns the process-wide context so background work honours
// shutdown. It never returns nil, which keeps it usable from tests that build
// a partial BotContext.
func (ctx *BotContext) context() context.Context {
	if ctx.ctx != nil {
		return ctx.ctx
	}
	return context.Background()
}

func NewBotContext() (*BotContext, error) {
	cfg := config.NewServiceConfig()

	ctxLogger := utils.Configure(utils.Config{
		ConsoleLoggingEnabled: true,
		EncodeLogsAsJson:      false,
		FileLoggingEnabled:    true,
		Directory:             cfg.BaseDir,
		Filename:              constant.LogFile,
		MaxSize:               10,
		MaxBackups:            10,
		MaxAge:                7,
		LogLevel:              cfg.LogLevel,
	})

	db, err := gorm.Open(sqlite.Open(path.Join(cfg.BaseDir, constant.DatabaseFile)), &gorm.Config{
		Logger: &dbLogger{ctxLogger},
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&model.Keyword{}, &model.History{}, &model.Alarm{}, &model.KeywordNotice{}); err != nil {
		return nil, err
	}
	dal.SetDefault(db)

	// Backfill: history rows written before links were absolute carry a
	// scheme-less URL, which would no longer match the claim key of new pushes
	// and make already delivered notices look new.
	if rows, err := dal.History.NormalizeURLs(); err != nil {
		ctxLogger.Error().Stack().Err(err).Msg("normalize history urls")
	} else if rows > 0 {
		ctxLogger.Info().Int64("rows", rows).Msg("normalized scheme-less history urls")
	}

	// The keyword-match log only has to cover the crawl window.
	if rows, err := dal.PruneKeywordMatches(time.Now()); err != nil {
		ctxLogger.Error().Stack().Err(err).Msg("prune keyword matches")
	} else if rows > 0 {
		ctxLogger.Debug().Int64("rows", rows).Msg("pruned keyword match records")
	}

	client, err := NewGotenbergClient(cfg.PDF.PDFServiceURL, cfg.PDF.WebhookURL(), cfg.PDF.WebhookToken)
	if err != nil {
		return nil, fmt.Errorf("create gotenberg client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	botContext := &BotContext{
		ctx:       ctx,
		cancel:    cancel,
		scheduler: gocron.NewScheduler(schedulerLocation()),
		Logger:    ctxLogger,
		Config:    cfg,
		Store:     NewStore[model.RequestInfo](),
		Gotenberg: client,
	}

	botContext.cmdHandler = NewCommandsHandler(botContext)

	telegramBot, err := bot.New(config.TelegramToken(), []bot.Option{
		bot.WithDefaultHandler((&defaultHandler{cmd: botContext.cmdHandler}).Handler),
		bot.WithHTTPClient(time.Minute, &http.Client{Timeout: 2 * time.Minute}),
	}...)
	if err != nil {
		return nil, err
	}
	botContext.Bot = telegramBot

	if err := botContext.initBot(); err != nil {
		return nil, err
	}

	processor, err := NewInfoProcessor(botContext)
	if err != nil {
		return nil, err
	}
	botContext.processor = processor
	return botContext, nil
}

// schedulerLocation returns the Asia/Shanghai location, falling back to a
// fixed +08:00 zone on hosts without tzdata (the distroless image ships none).
func schedulerLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*60*60)
}

func (ctx *BotContext) initBot() error {
	cmdHandler := ctx.cmdHandler
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Me, bot.MatchTypePrefix, MeHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Debug, bot.MatchTypePrefix, DebugHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Start, bot.MatchTypePrefix, (&startHandler{cmdHandler: cmdHandler}).Handler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.AddKeyword, bot.MatchTypePrefix, cmdHandler.AddKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.DeleteKeyword, bot.MatchTypePrefix, cmdHandler.DeleteKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.EditKeyword, bot.MatchTypePrefix, cmdHandler.EditKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.AddAlarmKeyword, bot.MatchTypePrefix, cmdHandler.AddAlarmKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.SearchAlarmRecords, bot.MatchTypePrefix, cmdHandler.SearchAlarmRecordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Alarm, bot.MatchTypePrefix, cmdHandler.AlarmRecordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.SearchHistory, bot.MatchTypePrefix, cmdHandler.SearchHistoryHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ListToday, bot.MatchTypePrefix, cmdHandler.ListTodayHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ConvertPDF, bot.MatchTypePrefix, cmdHandler.ConvertURLToPDFHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ConvertIMG, bot.MatchTypePrefix, cmdHandler.ConvertURLToIMGHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Statistics, bot.MatchTypePrefix, cmdHandler.StaticHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.SearchCallback, bot.MatchTypePrefix, cmdHandler.HandleCallbackQuery)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.AlarmCallback, bot.MatchTypePrefix, cmdHandler.HandleCallbackQuery)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.TodayCallback, bot.MatchTypePrefix, cmdHandler.HandleCallbackQuery)

	managerHandler := NewManagerHandler(ctx)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Retry, bot.MatchTypePrefix, managerHandler.Retry)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Clean, bot.MatchTypePrefix, managerHandler.Clean)

	_, err := ctx.Bot.SetMyCommands(ctx.context(), &bot.SetMyCommandsParams{
		Commands: TelegramCommands(),
	})
	return err
}

func (ctx *BotContext) Start() {
	scheduleInterval := config.ScheduleInterval()

	job, _ := ctx.scheduler.Every(scheduleInterval).Hours().Name("fetch_info").SingletonMode().Do(func() error {
		ctx.processor.Process()
		return nil
	})
	l := ctx.Logger.Debug()
	job.RegisterEventListeners(
		gocron.AfterJobRuns(func(jobName string) {
			l.Msgf("afterJobRuns: %s_scheduler", jobName)
		}),
		gocron.BeforeJobRuns(func(jobName string) {
			l.Msgf("beforeJobRuns: %s_scheduler", jobName)
		}),
		gocron.WhenJobReturnsError(func(jobName string, err error) {
			l.Msgf("whenJobReturnsError: %s_scheduler, %v", jobName, err)
		}),
		gocron.WhenJobReturnsNoError(func(jobName string) {
			l.Msgf("whenJobReturnsNoError: %s_scheduler", jobName)
		}),
	)

	ctx.scheduler.StartAsync()
	ctx.startWebhookServer()
	go ctx.Bot.Start(ctx.ctx)
}

func (ctx *BotContext) Stop() {
	if ctx.cancel != nil {
		ctx.cancel()
	}
	ctx.processor.Release()
	ctx.scheduler.Stop()
	ctx.scheduler.StopBlockingChan()
	if ctx.shutdownWebhook != nil {
		ctx.shutdownWebhook()
	}
}

// startWebhookServer serves the Gotenberg callback endpoint. The request id is
// a path wildcard, so the handler can reject unknown ids before reading a body.
func (ctx *BotContext) startWebhookServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+constant.PDFEndPoint+"{id}", newWebhooker(ctx).WebhookHandler)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", ctx.Config.PDF.WebhookServerPort),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		WriteTimeout:      time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	go func() {
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			ctx.Logger.Error().Err(err).Msg("Webhook server error")
		}
	}()

	ctx.shutdownWebhook = func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			ctx.Logger.Error().Err(err).Msg("Webhook server shutdown error")
		}
	}
}
