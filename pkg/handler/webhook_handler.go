package handler

import (
	"bytes"
	"io"
	"net/http"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/model"
)

const (
	webhookMaxBodyBytes = 20 << 20 // 20MB, matching Telegram document upload limit
	webhookMaxWorkers   = 10
)

type webhooker struct {
	ctx *BotContext
	sem chan struct{}
}

func newWebhooker(ctx *BotContext) *webhooker {
	return &webhooker{ctx: ctx, sem: make(chan struct{}, webhookMaxWorkers)}
}

// WebhookHandler receives the converted file of one conversion request. The
// request id is the {id} path wildcard, so an unknown id is rejected before
// its body is read.
func (wh *webhooker) WebhookHandler(w http.ResponseWriter, r *http.Request) {
	if token := wh.ctx.Config.PDF.WebhookToken; token != "" && r.URL.Query().Get("token") != token {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	requestID := r.PathValue("id")
	req, found := wh.ctx.Store.Get(requestID)
	if !found {
		wh.ctx.Logger.Error().Str("requestId", requestID).Msg("Chat ID not found for request")
		http.Error(w, "Chat ID not found", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, webhookMaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		wh.ctx.Logger.Error().Stack().Err(err).Msg("")
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	go wh.deliver(req, body)
	w.WriteHeader(http.StatusOK)
}

// deliver replaces the "please wait" message with the converted file.
func (wh *webhooker) deliver(req model.RequestInfo, data []byte) {
	select {
	case wh.sem <- struct{}{}:
		defer func() { <-wh.sem }()
	default:
		wh.ctx.Logger.Warn().Msg("webhook worker pool full, dropping request")
		return
	}

	ctx := wh.ctx.context()
	if _, err := wh.ctx.Bot.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    req.ChatId,
		MessageID: req.MessageId,
	}); err != nil {
		wh.ctx.Logger.Error().Msgf("Failed to delete message: %v", err)
	}

	switch req.Type {
	case model.PDF:
		if _, err := wh.ctx.Bot.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID: req.ChatId,
			Document: &models.InputFileUpload{
				Filename: req.FileName,
				Data:     bytes.NewReader(data),
			},
			Caption: req.Message,
			ReplyParameters: &models.ReplyParameters{
				MessageID: req.ReplyMessageId,
			},
		}); err != nil {
			wh.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	case model.IMG:
		if _, err := wh.ctx.Bot.SendPhoto(ctx, &bot.SendPhotoParams{
			ChatID: req.ChatId,
			Photo: &models.InputFileUpload{
				Filename: req.FileName,
				Data:     bytes.NewReader(data),
			},
			Caption: req.Message,
			ReplyParameters: &models.ReplyParameters{
				MessageID: req.ReplyMessageId,
			},
		}); err != nil {
			wh.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}
}
