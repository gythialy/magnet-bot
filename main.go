package main

import (
	"log"
	"os"
	"os/signal"
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
	configHandler := handler.NewCommandsHandler(ctx)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.MAGNET, bot.MatchTypePrefix, handler.NewMagnetHandler(ctx).Handler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.ME, bot.MatchTypePrefix, handler.MeHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.AddKeyword, bot.MatchTypePrefix, configHandler.AddKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.DeleteKeyword, bot.MatchTypePrefix, configHandler.DeleteKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.ListKeyword, bot.MatchTypePrefix, configHandler.ListKeywordHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.AddTenderCode, bot.MatchTypePrefix, configHandler.AddTenderCodeHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.DeleteTenderCode, bot.MatchTypePrefix, configHandler.DeleteTenderCodeHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.ListTenderCode, bot.MatchTypePrefix, configHandler.ListTenderCodeHandler)
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, handler.RETRY, bot.MatchTypePrefix, handler.NewManagerHandler(ctx).Retry)

	job, _ := ctx.Scheduler.Every(1).Days().At("10:30").Name("fetch_info").Do(func() error {
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
