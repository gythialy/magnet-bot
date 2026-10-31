package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/handler"
)

func main() {
	fmt.Printf("magnet %s @ %s\n", constant.Version, constant.BuildTime)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	botContext, err := handler.NewBotContext()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("NewBotContext")
	}

	if err := botContext.Start(); err != nil {
		log.Fatal().Stack().Err(err).Msg("start")
	}
	<-ctx.Done()
	botContext.Stop()
}
