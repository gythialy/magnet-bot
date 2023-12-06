package handler

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
	"github.com/nmmh/magneturi/magneturi"
	"log"
	"net/url"
	"os"
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

var splitRegex = regexp.MustCompile("\r?\n")

type MagnetHandler struct {
	ctx *pkg.BotContext
}

func NewMagnetHandler(ctx *pkg.BotContext) *MagnetHandler {
	return &MagnetHandler{
		ctx: ctx,
	}
}

func (m *MagnetHandler) Handler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimPrefix(text, MAGNET)
	urls := splitRegex.Split(tmp, -1)
	server := m.fetchServer()
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

func (m *MagnetHandler) fetchServer() string {
	f := filepath.Join(m.ctx.BaseDir, BestFile)
	logger := m.ctx.Logger
	logger.Info().Msgf("file: %s", f)

	if s, err := os.Stat(f); errors.Is(err, os.ErrNotExist) || s.ModTime().Add(time.Hour*24).Before(time.Now()) {
		_ = os.Remove(f)
		resty.New().EnableTrace().R().SetOutput(f).Get(BestUrlFile)
	}

	data, err := os.ReadFile(f)
	if err != nil {
		logger.Err(err)
	}
	lines := splitRegex.Split(string(data), -1)
	sb := strings.Builder{}
	for _, line := range lines {
		if line != "" {
			sb.WriteString(fmt.Sprintf("&tr=%s", url.QueryEscape(line)))
		}
	}
	return sb.String()
}
