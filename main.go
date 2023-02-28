package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/cavaliergopher/grab/v3"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/constant"
	"github.com/nmmh/magneturi/magneturi"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	BestUrlFile = "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt"
	BestFile    = "trackers_best.txt"
	MAGNET      = "/magnet"
)

var SplitRegex = regexp.MustCompile("\r?\n")

func main() {
	log.Printf("magnet %s @ %s\n", constant.Version, constant.BuildTime)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		bot.WithDefaultHandler(defaultHandler),
	}

	b, _ := bot.New(os.Getenv("TELEGRAM_BOT_TOKEN"), opts...)

	b.RegisterHandler(bot.HandlerTypeMessageText, MAGNET, bot.MatchTypePrefix, magnetHandler)

	b.Start(ctx)
}

func magnetHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimPrefix(text, MAGNET)
	urls := SplitRegex.Split(tmp, -1)
	server := fetchServer()
	result := strings.Builder{}
	for _, url := range urls {
		u := strings.TrimSpace(url)
		if u != "" {
			uri, err := magneturi.Parse(u, true)
			if err != nil {
				log.Println(err)
			}
			filter, err := uri.Filter("xt", "dn", "tr")
			if err != nil {
				log.Println(err)
				continue
			}
			result.WriteString(filter.String() + server + "\n")
		}
	}

	if result.Len() == 0 {
		result.WriteString("No links found")
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   result.String(),
	})
}

func defaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "/magnet append tracker servers",
	})
}

func fetchServer() string {
	dir, _ := os.Executable()
	f := filepath.Join(path.Dir(dir), BestFile)

	log.Printf("file: %s", f)

	if s, err := os.Stat(f); errors.Is(err, os.ErrNotExist) || s.ModTime().Add(time.Hour*24).Before(time.Now()) {
		_ = os.Remove(f)
		// create client
		client := grab.NewClient()
		req, _ := grab.NewRequest(f, BestUrlFile)

		// start download
		log.Printf("Downloading %v...\n", req.URL())
		resp := client.Do(req)
		log.Printf("%v\n", resp.HTTPResponse.Status)

		// start UI loop
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()

	Loop:
		for {
			select {
			case <-t.C:
				log.Printf("transferred %d / %d bytes (%.2f%%)\n",
					resp.BytesComplete(), resp.Size(), 100*resp.Progress())

			case <-resp.Done:
				// download is complete
				break Loop
			}
		}

		// check for errors
		if err := resp.Err(); err != nil {
			log.Fatalf("Download failed: %v\n", err)
		}

		log.Printf("%s saved to %s \n", BestFile, resp.Filename)
	}

	data, err := os.ReadFile(f)
	if err != nil {
		log.Fatal(err)
	}
	lines := SplitRegex.Split(string(data), -1)
	sb := strings.Builder{}
	for _, line := range lines {
		if line != "" {
			sb.WriteString(fmt.Sprintf("&tr=%s", url.QueryEscape(line)))
		}
	}
	return sb.String()
}
