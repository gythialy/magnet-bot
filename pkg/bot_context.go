package pkg

import (
	"os"
	"path"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm/logger"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot/models"

	"github.com/go-co-op/gocron"
	"github.com/go-telegram/bot"
	"github.com/gythialy/magnet/pkg/entities"
	"github.com/gythialy/magnet/pkg/utiles"
	"golang.org/x/net/context"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type dbLogger struct {
	*utiles.Logger
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
	ctx       context.Context
	cancel    context.CancelFunc
	Bot       *bot.Bot
	DB        *gorm.DB
	Scheduler *gocron.Scheduler
	ManagerId int64
	ServerUrl string
	Processor *InfoProcessor
	Logger    *utiles.Logger
	BaseDir   string
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
				Description: "Delete keywords",
			},
			{
				Command:     constant.ListKeyword,
				Description: "List keywords",
			},
			{
				Command:     constant.AddAlarmKeyword,
				Description: "Add tender codes",
			},
			{
				Command:     constant.DeleteAlarmKeyword,
				Description: "Delete alarm keywords",
			},
			{
				Command:     constant.ListAlarmKeyword,
				Description: "List alarm keywords",
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
	ctxLogger := utiles.Configure(utiles.Config{
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

	err = db.AutoMigrate(&entities.Keyword{}, &entities.History{}, &entities.Alarm{})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	botContext := &BotContext{
		ctx:       ctx,
		cancel:    cancel,
		Scheduler: gocron.NewScheduler(time.FixedZone("CST", 8*60*60)),
		Bot:       telegramBot,
		DB:        db,
		ManagerId: id(),
		ServerUrl: os.Getenv(constant.ServerURL),
		Logger:    ctxLogger,
		BaseDir:   cfgPath,
	}
	if botContext.Processor, err = NewInfoProcessor(botContext); err == nil {
		return botContext, nil
	} else {
		return nil, err
	}
}

func (ctx *BotContext) Start() {
	ctx.Scheduler.StartAsync()
	go ctx.Bot.Start(ctx.ctx)
}

func (ctx *BotContext) Stop() {
	ctx.cancel()
	ctx.Processor.Release()
	ctx.Scheduler.Stop()
	ctx.Scheduler.StopBlockingChan()
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
