package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/go-co-op/gocron"
	"github.com/go-telegram/bot"
	"github.com/gythialy/magnet/pkg"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/handler"
)

func main() {
	log.Printf("magnet %s @ %s\n", constant.Version, constant.BuildTime)

	ctx, err := pkg.NewBotContext()
	if err != nil {
		log.Printf("error: %v\n", err)
	}
	commandsHandler := handler.NewCommandsHandler(ctx)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Magnet, bot.MatchTypePrefix, handler.NewMagnetHandler(ctx).Handler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Me, bot.MatchTypePrefix, handler.MeHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.AddKeyword, bot.MatchTypePrefix, commandsHandler.AddKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.DeleteKeyword, bot.MatchTypePrefix, commandsHandler.DeleteKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ListKeyword, bot.MatchTypePrefix, commandsHandler.ListKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.AddAlarmKeyword, bot.MatchTypePrefix, commandsHandler.AddAlarmKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.DeleteAlarmKeyword, bot.MatchTypePrefix, commandsHandler.DeleteAlarmKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ListAlarmKeyword, bot.MatchTypePrefix, commandsHandler.ListAlarmKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ListAlarmRecords, bot.MatchTypePrefix, commandsHandler.ListAlarmRecordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.SearchHistory, bot.MatchTypePrefix, commandsHandler.SearchHistoryHandler)
	managerHandler := handler.NewManagerHandler(ctx)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Retry, bot.MatchTypePrefix, managerHandler.Retry)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Clean, bot.MatchTypePrefix, managerHandler.Clean)
	scheduleInterval := 1
	interval := os.Getenv("SCHEDULE_INTERVAL")
	if interval != "" {
		if i, err := strconv.Atoi(interval); err == nil {
			scheduleInterval = i
		}
	}
	job, _ := ctx.Scheduler.Every(scheduleInterval).Hours().Name("fetch_info").Do(func() error {
		ctx.Processor.Process()
		return nil
	})
	job.RegisterEventListeners(
		gocron.AfterJobRuns(func(jobName string) {
			logger := ctx.Logger.Info()
			logger.Msgf("afterJobRuns: %scheduler", jobName)
		}),
		gocron.BeforeJobRuns(func(jobName string) {
			log.Printf("beforeJobRuns: %scheduler\n", jobName)
		}),
		gocron.WhenJobReturnsError(func(jobName string, err error) {
			log.Printf("whenJobReturnsError: %scheduler, %v\n", jobName, err)
		}),
		gocron.WhenJobReturnsNoError(func(jobName string) {
			log.Printf("whenJobReturnsNoError: %scheduler\n", jobName)
		}),
	)

	ctx.Start()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	<-signalChan

	ctx.Stop()
}
