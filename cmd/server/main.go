package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/gythialy/magnet/pkg/handler"

	"github.com/gythialy/magnet/pkg/constant"
)

func main() {
	fmt.Printf("magnet %s @ %s\n", constant.Version, constant.BuildTime)

	ctx, err := handler.NewBotContext()
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("NewBotContext")
	}

	ctx.Start()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	<-signalChan

	ctx.Stop()
}
