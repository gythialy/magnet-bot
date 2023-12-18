package pkg

import (
	"os"
	"path"
	"strconv"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/go-co-op/gocron"
	"github.com/go-telegram/bot"
	"github.com/gythialy/magnet/pkg/entities"
	"github.com/gythialy/magnet/pkg/utiles"
	"golang.org/x/net/context"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	TelegramBotToken = "TELEGRAM_BOT_TOKEN"
	ConfigPath       = "CONFIG_PATH"
	ManagerId        = "MANAGER_ID"
	ServerURL        = "SERVER_URL"
	DatabaseFile     = "bot.db"
	logFile          = "bot.log"
)

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
	cfgPath := os.Getenv(ConfigPath)
	if cfgPath == "" {
		cfgPath, _ = os.Getwd()
	}
	telegramBot, err := bot.New(os.Getenv(TelegramBotToken), []bot.Option{}...)
	if err != nil {
		return nil, err
	}

	if _, err := telegramBot.SetMyCommands(context.Background(), &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{
				Command:     "/me",
				Description: "Get my user information",
			},
			{
				Command:     "/magnet",
				Description: "Append trackers to torrent",
			},
			{
				Command:     "/add_keywords",
				Description: "Add keywords",
			},
			{
				Command:     "/delete_keywords",
				Description: "Delete keywords",
			},
			{
				Command:     "/list_keywords",
				Description: "List keywords",
			},
			{
				Command:     "/add_tender_codes",
				Description: "Add tender codes",
			},
			{
				Command:     "/delete_tender_codes",
				Description: "Delete tender codes",
			},
			{
				Command:     "/list_tender_codes",
				Description: "List tender codes",
			},
			{
				Command:     "/retry",
				Description: "Retry failed task, only for the Bot master",
			},
			{
				Command:     "/clean",
				Description: "Clean the cache file, only for the Bot master",
			},
		},
	}); err != nil {
		return nil, err
	}

	db, err := gorm.Open(sqlite.Open(path.Join(cfgPath, DatabaseFile)), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&entities.Keyword{}, &entities.TenderCode{}, &entities.History{})
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
		ServerUrl: os.Getenv(ServerURL),
		Logger: utiles.Configure(utiles.Config{
			ConsoleLoggingEnabled: true,
			EncodeLogsAsJson:      false,
			FileLoggingEnabled:    true,
			Directory:             cfgPath,
			Filename:              logFile,
			MaxSize:               10,
			MaxBackups:            10,
			MaxAge:                7,
		}),
		BaseDir: cfgPath,
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
	id := os.Getenv(ManagerId)
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
