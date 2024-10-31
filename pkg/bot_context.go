package pkg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/rs/zerolog"
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

type PDFServiceConfig struct {
	WebhookServer     string
	WebhookServerPort int
	PDFServiceURL     string
}

func (c *PDFServiceConfig) Init() *PDFServiceConfig {
	if u := os.Getenv(constant.PDFServerUrl); u != "" {
		c.PDFServiceURL = u
	}
	if u := os.Getenv(constant.WebhookServerURL); u != "" {
		c.WebhookServer = u
	}

	if v := os.Getenv(constant.WebhookServerPort); v != "" {
		c.WebhookServerPort, _ = strconv.Atoi(v)
	}
	return c
}

func (c *PDFServiceConfig) WebhookURL() string {
	return fmt.Sprintf("%s:%d", c.WebhookServer, c.WebhookServerPort)
}

type BotContext struct {
	ctx              context.Context
	cancel           context.CancelFunc
	Bot              *bot.Bot
	Store            *Store
	Scheduler        *gocron.Scheduler
	ManagerId        int64
	MessageServerUrl string
	Processor        *InfoProcessor
	Logger           *utils.Logger
	BaseDir          string
	PDFServiceConfig *PDFServiceConfig
	shutdownWebhook  func()
}

func NewBotContext() (*BotContext, error) {
	cfgPath := os.Getenv(constant.ConfigPath)
	if cfgPath == "" {
		cfgPath, _ = os.Getwd()
	}
	telegramBot, err := bot.New(os.Getenv(constant.TelegramBotToken), []bot.Option{}...)
	if err != nil {
		return nil, err
	}

	if _, err := telegramBot.SetMyCommands(context.Background(), &bot.SetMyCommandsParams{
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
				Command:     constant.Static,
				Description: "Show static information data",
			},
			{
				Command:     constant.ConvertPDF,
				Description: "Convert URL to PDF",
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
		return nil, err
	}

	level := logLevel()
	ctxLogger := utils.Configure(utils.Config{
		ConsoleLoggingEnabled: true,
		EncodeLogsAsJson:      false,
		FileLoggingEnabled:    true,
		Directory:             cfgPath,
		Filename:              constant.LogFile,
		MaxSize:               10,
		MaxBackups:            10,
		MaxAge:                7,
		LogLevel:              level,
	})

	db, err := gorm.Open(sqlite.Open(path.Join(cfgPath, constant.DatabaseFile)), &gorm.Config{
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

	ctx, cancel := context.WithCancel(context.Background())

	pdfServiceConfig := &PDFServiceConfig{}
	botContext := &BotContext{
		ctx:              ctx,
		cancel:           cancel,
		Scheduler:        gocron.NewScheduler(time.FixedZone("CST", 8*60*60)),
		Bot:              telegramBot,
		Store:            NewStore(),
		ManagerId:        id(),
		MessageServerUrl: os.Getenv(constant.ServerURL),
		PDFServiceConfig: pdfServiceConfig.Init(),
		Logger:           ctxLogger,
		BaseDir:          cfgPath,
	}
	if botContext.Processor, err = NewInfoProcessor(botContext); err == nil {
		return botContext, nil
	} else {
		return nil, err
	}
}

func (ctx *BotContext) Start() {
	ctx.Scheduler.StartAsync()
	ctx.startWebhookServer()
	go ctx.Bot.Start(ctx.ctx)
}

func (ctx *BotContext) Stop() {
	ctx.cancel()
	ctx.Processor.Release()
	ctx.Scheduler.Stop()
	ctx.Scheduler.StopBlockingChan()
	ctx.shutdownWebhook()
}

func (ctx *BotContext) startWebhookServer() {
	server := &http.Server{Addr: fmt.Sprintf(":%d", ctx.PDFServiceConfig.WebhookServerPort)}

	http.HandleFunc(constant.PDFEndPoint, func(w http.ResponseWriter, r *http.Request) {
		requestID := r.URL.Path[len(constant.PDFEndPoint):]

		body, err := io.ReadAll(r.Body)
		if err != nil {
			ctx.Logger.Error().Msg(err.Error())
			http.Error(w, "Failed to read body", http.StatusInternalServerError)
			return
		}

		if ri, found := ctx.Store.Get(requestID); !found {
			ctx.Logger.Error().Msg("Chat ID not found for request")
			http.Error(w, "Chat ID not found", http.StatusNotFound)
		} else {
			req := ri.(model.RequestInfo)
			go func(req model.RequestInfo, pdfData []byte) {
				// delete the processing message
				_, err := ctx.Bot.DeleteMessage(context.Background(), &bot.DeleteMessageParams{
					ChatID:    req.ChatId,
					MessageID: req.MessageId,
				})
				if err != nil {
					ctx.Logger.Error().Msgf("Failed to delete message: %v", err)
				}

				// Send the PDF file
				if _, err := ctx.Bot.SendDocument(context.Background(), &bot.SendDocumentParams{
					ChatID: req.ChatId,
					Document: &models.InputFileUpload{
						Filename: req.FileName,
						Data:     bytes.NewReader(pdfData),
					},
					Caption: req.Message,
				}); err != nil {
					ctx.Logger.Error().Msg(err.Error())
				}
			}(req, body)

			w.WriteHeader(http.StatusOK)
		}
	})

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

func id() int64 {
	id := os.Getenv(constant.ManagerId)
	if id == "" {
		return 0
	} else {
		if i, err := strconv.ParseInt(id, 10, 64); err == nil {
			return i
		} else {
			return 0
		}
	}
}

func logLevel() zerolog.Level {
	logLevel := os.Getenv(constant.LogLevel)
	if logLevel != "" {
		if level, err := zerolog.ParseLevel(logLevel); err == nil {
			return level
		}
	}
	return zerolog.DebugLevel
}
