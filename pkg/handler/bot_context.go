package handler

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"time"

	"github.com/gythialy/magnet/pkg/config"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"gorm.io/gorm/logger"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot/models"

	"github.com/glebarez/sqlite"
	"github.com/go-co-op/gocron"
	"github.com/go-telegram/bot"
	"github.com/gythialy/magnet/pkg/utils"
	"golang.org/x/net/context"
	"gorm.io/gorm"
)

type dbLogger struct {
	*utils.Logger
}

func (dl *dbLogger) LogMode(_ logger.LogLevel) logger.Interface {
	return dl
}

func (dl *dbLogger) Info(_ context.Context, msg string, data ...interface{}) {
	dl.Logger.Info().Msgf(msg, data...)
}

func (dl *dbLogger) Warn(_ context.Context, msg string, data ...interface{}) {
	dl.Logger.Warn().Msgf(msg, data...)
}

func (dl *dbLogger) Error(_ context.Context, msg string, data ...interface{}) {
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
	Store           *Store
	Logger          *utils.Logger
	Config          *config.ServiceConfig
	Gotenberg       *GotenbergClient
	ctx             context.Context
	cancel          context.CancelFunc
	scheduler       *gocron.Scheduler
	processor       *InfoProcessor
	shutdownWebhook func()
}

func NewBotContext() (*BotContext, error) {
	cfg := config.NewServiceConfig()

	telegramBot, err := bot.New(config.TelegramToken(), []bot.Option{
		bot.WithDefaultHandler(DefaultHandler),
		bot.WithHTTPClient(time.Minute, &http.Client{
			Timeout: 2 * time.Minute,
		}),
		// bot.WithDebug(),
	}...)
	if err != nil {
		return nil, err
	}

	level := cfg.LogLevel
	ctxLogger := utils.Configure(utils.Config{
		ConsoleLoggingEnabled: true,
		EncodeLogsAsJson:      false,
		FileLoggingEnabled:    true,
		Directory:             cfg.BaseDir,
		Filename:              constant.LogFile,
		MaxSize:               10,
		MaxBackups:            10,
		MaxAge:                7,
		LogLevel:              level,
	})

	db, err := gorm.Open(sqlite.Open(path.Join(cfg.BaseDir, constant.DatabaseFile)), &gorm.Config{
		Logger: &dbLogger{ctxLogger},
	})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&model.Keyword{}, &model.History{}, &model.Alarm{})
	if err != nil {
		return nil, err
	}
	dal.SetDefault(db)

	client, err := NewGotenbergClient(cfg.PDF.PDFServiceURL, cfg.PDF.WebhookURL())
	if err != nil {
		return nil, fmt.Errorf("failed to create gotenberg client: %s", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	botContext := &BotContext{
		ctx:       ctx,
		cancel:    cancel,
		scheduler: gocron.NewScheduler(time.FixedZone("CST", 8*60*60)),
		Bot:       telegramBot,
		Gotenberg: client,
		Store:     NewStore(),
		Logger:    ctxLogger,
		Config:    cfg,
	}
	if err = botContext.initBot(); err != nil {
		return nil, err
	}
	if botContext.processor, err = NewInfoProcessor(botContext); err == nil {
		return botContext, nil
	} else {
		return nil, err
	}
}

func (ctx *BotContext) initBot() error {
	cmdHandler := NewCommandsHandler(ctx)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Magnet, bot.MatchTypePrefix, NewMagnetHandler(ctx).Handler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Me, bot.MatchTypePrefix, MeHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.AddKeyword, bot.MatchTypePrefix, cmdHandler.AddKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.DeleteKeyword, bot.MatchTypePrefix, cmdHandler.DeleteKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.EditKeyword, bot.MatchTypePrefix, cmdHandler.EditKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.AddAlarmKeyword, bot.MatchTypePrefix, cmdHandler.AddAlarmKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ListAlarmRecords, bot.MatchTypePrefix, cmdHandler.ListAlarmRecordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.SearchHistory, bot.MatchTypePrefix, cmdHandler.SearchHistoryHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ConvertPDF, bot.MatchTypePrefix, cmdHandler.ConvertURLToPDFHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ConvertIMG, bot.MatchTypePrefix, cmdHandler.ConvertURLToIMGHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Statistics, bot.MatchTypePrefix, cmdHandler.StaticHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.Search, bot.MatchTypePrefix, cmdHandler.HandleCallbackQuery)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.Alarm, bot.MatchTypePrefix, cmdHandler.HandleCallbackQuery)

	managerHandler := NewManagerHandler(ctx)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Retry, bot.MatchTypePrefix, managerHandler.Retry)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Clean, bot.MatchTypePrefix, managerHandler.Clean)

	if _, err := ctx.Bot.SetMyCommands(context.Background(), &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{
				Command:     constant.Me,
				Description: "Get my user information",
			},
			{
				Command:     constant.Magnet,
				Description: "Append trackers to torrent",
			},
			{
				Command:     constant.AddKeyword,
				Description: "Add keywords",
			},
			{
				Command:     constant.DeleteKeyword,
				Description: "Delete keywords by record ids",
			},
			{
				Command:     constant.EditKeyword,
				Description: "Edit keywords by record id",
			},
			{
				Command:     constant.AddAlarmKeyword,
				Description: "Add tender codes",
			},
			{
				Command:     constant.ListAlarmRecords,
				Description: "List all alarm records",
			},
			{
				Command:     constant.SearchHistory,
				Description: "Search history data by title",
			},
			{
				Command:     constant.Statistics,
				Description: "Show statistics information",
			},
			{
				Command:     constant.ConvertPDF,
				Description: "Convert URL to PDF",
			},
			{
				Command:     constant.ConvertIMG,
				Description: "Convert URL to IMG",
			},
			{
				Command:     constant.Retry,
				Description: "Retry failed task, only for the Bot master",
			},
			{
				Command:     constant.Clean,
				Description: "Clean the cache file, only for the Bot master",
			},
		},
	}); err != nil {
		return err
	} else {
		return nil
	}
}

func (ctx *BotContext) Start() {
	scheduleInterval := config.ScheduleInterval()

	job, _ := ctx.scheduler.Every(scheduleInterval).Hours().Name("fetch_info").Do(func() error {
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
	ctx.cancel()
	ctx.processor.Release()
	ctx.scheduler.Stop()
	ctx.scheduler.StopBlockingChan()
	ctx.shutdownWebhook()
}

func (ctx *BotContext) startWebhookServer() {
	server := &http.Server{Addr: fmt.Sprintf(":%d", ctx.Config.PDF.WebhookServerPort)}

	http.HandleFunc(constant.PDFEndPoint, newWebhooker(ctx).WebhookHandler)

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
