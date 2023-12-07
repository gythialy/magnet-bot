package pkg

import (
	"github.com/go-co-op/gocron"
	"github.com/go-telegram/bot"
	"github.com/gythialy/magnet/pkg/entities"
	"github.com/gythialy/magnet/pkg/utiles"
	"golang.org/x/net/context"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"os"
	"path"
	"strconv"
	"time"
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
	db, err := gorm.Open(sqlite.Open(path.Join(cfgPath, DatabaseFile)), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	db.AutoMigrate(&entities.Keyword{}, &entities.TenderCode{})

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