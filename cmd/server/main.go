package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gythialy/magnet/pkg/handler"

	"github.com/gythialy/magnet/pkg/constant"
)

func main() {
	log.Printf("magnet %s @ %s\n", constant.Version, constant.BuildTime)

	ctx, err := handler.NewBotContext()
	if err != nil {
		log.Fatal(err)
	}

	ctx.Start()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	<-signalChan

	ctx.Stop()
}
