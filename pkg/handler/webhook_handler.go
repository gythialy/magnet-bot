package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/model"
)

type webhooker struct {
	ctx *BotContext
}

func newWebhooker(ctx *BotContext) *webhooker {
	return &webhooker{ctx: ctx}
}

func (wh *webhooker) WebhookHandler(w http.ResponseWriter, r *http.Request) {
	requestID := r.URL.Path[len(constant.PDFEndPoint):]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		wh.ctx.Logger.Error().Stack().Err(err).Msg("")
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	if ri, found := wh.ctx.Store.Get(requestID); !found {
		wh.ctx.Logger.Error().Msg("Chat ID not found for request")
		http.Error(w, "Chat ID not found", http.StatusNotFound)
	} else {
		req := ri.(model.RequestInfo)
		go func(req model.RequestInfo, data []byte) {
			switch req.Type {
			case model.PDF:
				// delete the processing message
				_, err := wh.ctx.Bot.DeleteMessage(context.Background(), &bot.DeleteMessageParams{
					ChatID:    req.ChatId,
					MessageID: req.MessageId,
				})
				if err != nil {
					wh.ctx.Logger.Error().Msgf("Failed to delete message: %v", err)
				}

				// Send the PDF file
				if _, err := wh.ctx.Bot.SendDocument(context.Background(), &bot.SendDocumentParams{
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
				_, err := wh.ctx.Bot.DeleteMessage(context.Background(), &bot.DeleteMessageParams{
					ChatID:    req.ChatId,
					MessageID: req.MessageId,
				})
				if err != nil {
					wh.ctx.Logger.Error().Msgf("Failed to delete message: %v", err)
				}
				if _, err := wh.ctx.Bot.SendPhoto(context.Background(), &bot.SendPhotoParams{
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
		}(req, body)

		w.WriteHeader(http.StatusOK)
	}
}
