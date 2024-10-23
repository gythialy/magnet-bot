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

const defaultScheduleInterval = 1

func main() {
	log.Printf("magnet %s @ %s\n", constant.Version, constant.BuildTime)

	ctx, err := pkg.NewBotContext()
	if err != nil {
		log.Fatal(err)
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
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.ConvertPDF, bot.MatchTypePrefix, commandsHandler.ConvertURLToPDFHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.Search+":", bot.MatchTypePrefix, commandsHandler.HandleCallbackQuery)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.Alarm+":", bot.MatchTypePrefix, commandsHandler.HandleCallbackQuery)
	managerHandler := handler.NewManagerHandler(ctx)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Retry, bot.MatchTypePrefix, managerHandler.Retry)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, constant.Clean, bot.MatchTypePrefix, managerHandler.Clean)

	scheduleInterval := defaultScheduleInterval
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
	logger := ctx.Logger.Debug()
	job.RegisterEventListeners(
		gocron.AfterJobRuns(func(jobName string) {
			logger.Msgf("afterJobRuns: %scheduler", jobName)
		}),
		gocron.BeforeJobRuns(func(jobName string) {
			logger.Msgf("beforeJobRuns: %scheduler", jobName)
		}),
		gocron.WhenJobReturnsError(func(jobName string, err error) {
			logger.Msgf("whenJobReturnsError: %scheduler, %v", jobName, err)
		}),
		gocron.WhenJobReturnsNoError(func(jobName string) {
			logger.Msgf("whenJobReturnsNoError: %scheduler", jobName)
		}),
	)

	ctx.Start()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	<-signalChan

	ctx.Stop()
}
