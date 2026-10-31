package handler

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/go-telegram/bot"

	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/model"
)

const (
	// conversionSubmitTimeout bounds the Gotenberg submission. Gotenberg
	// answers a webhook request with 204 as soon as it has accepted the job, so
	// a healthy submission is fast; the bound keeps a wedged Gotenberg from
	// blocking the bot worker (or an HTTP request) for the client's full
	// three-minute timeout.
	conversionSubmitTimeout = 30 * time.Second

	// conversionFailureNoticeTimeout bounds the edit that replaces the
	// placeholder. It is short because it only needs to reach Telegram, and it
	// runs on a detached context so a cancelled caller does not leave the chat
	// staring at a "please wait" message forever.
	conversionFailureNoticeTimeout = 10 * time.Second
)

// ErrDomainNotAllowed reports a notice URL outside the configured server. Both
// callers turn it into a user-visible rejection, so the text is part of the
// contract rather than a diagnostic.
var ErrDomainNotAllowed = errors.New("URL domain is not allowed")

// StartConversion submits one asynchronous conversion and returns the request
// id the webhook will use to claim the result.
//
// It runs the same steps for the chat commands and the Mini App: enforce the
// notice host allow-list, derive the output file name, post the "please wait"
// placeholder, submit the job to Gotenberg, and record the request. The file
// itself still arrives out of band on POST /pdf/{id}; only the submission is
// synchronous, because the caller must learn whether the job was accepted (and
// the placeholder has to be swapped for a failure notice when it was not).
func (c *BotContext) StartConversion(ctx context.Context, chatID int64, noticeURL string,
	kind model.FileType, replyMessageID int,
) (string, error) {
	extension, label, ok := conversionFormat(kind)
	if !ok {
		return "", fmt.Errorf("unsupported conversion type %d", kind)
	}

	parsedURL, err := url.Parse(noticeURL)
	if err != nil {
		return "", fmt.Errorf("parse notice url: %w", err)
	}
	// Only notices from the configured server may be converted: Gotenberg
	// fetches whatever URL it is given, so this is the allow-list that keeps
	// it from being pointed at arbitrary hosts.
	if parsedURL.Host != c.Config.MessageServerUrl {
		return "", ErrDomainNotAllowed
	}

	fileName := ""
	if name, nameErr := extractFileName(ctx, parsedURL); nameErr == nil {
		fileName = name + extension
	} else {
		c.Logger.Error().Stack().Err(nameErr).Msg("derive conversion file name")
	}

	placeholder, err := c.Bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Converting URL to %s(%s). Please wait...⌛", label, fileName),
	})
	if err != nil {
		c.Logger.Error().Stack().Err(err).Msg("send conversion placeholder")
		return "", fmt.Errorf("send conversion placeholder: %w", err)
	}

	request := model.RequestInfo{
		ChatId:         chatID,
		MessageId:      placeholder.ID,
		Message:        parsedURL.String(),
		ReplyMessageId: replyMessageID,
		FileName:       fileName,
		Type:           kind,
	}

	submitCtx, cancel := context.WithTimeout(ctx, conversionSubmitTimeout)
	defer cancel()

	requestID, err := c.submitConversion(submitCtx, kind, request.Message)
	if err != nil {
		c.Logger.Error().Stack().Err(err).Msg("submit conversion")
		c.editConversionFailure(ctx, chatID, placeholder.ID, label)
		return "", fmt.Errorf("submit conversion: %w", err)
	}

	c.Store.Set(requestID, request, DefaultCacheDuration)
	return requestID, nil
}

// submitConversion asks Gotenberg to convert one URL into the requested type.
// The switch mirrors the two conversion kinds exactly; an unknown kind is a
// programming error rather than a conversion that silently never starts.
func (c *BotContext) submitConversion(ctx context.Context, kind model.FileType, noticeURL string) (string, error) {
	switch kind {
	case model.PDF:
		return c.Gotenberg.URLToPDF(ctx, noticeURL)
	case model.IMG:
		return c.Gotenberg.URLToImage(ctx, noticeURL)
	default:
		return "", fmt.Errorf("unsupported conversion type %d", kind)
	}
}

// conversionFormat maps a conversion kind to its file extension and the label
// shown in the placeholder. Keeping the two together stops the command and the
// Mini App from drifting to different wording or file types.
func conversionFormat(kind model.FileType) (extension, label string, ok bool) {
	switch kind {
	case model.PDF:
		return constant.PDFExtension, "PDF", true
	case model.IMG:
		return constant.ImgExtension, "IMG", true
	default:
		return "", "", false
	}
}

// editConversionFailure replaces the placeholder when Gotenberg rejects the
// job. The edit is best effort: a failure here is logged and does not change
// the error the caller already received.
func (c *BotContext) editConversionFailure(ctx context.Context, chatID int64, messageID int, label string) {
	editCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), conversionFailureNoticeTimeout)
	defer cancel()

	if _, err := c.Bot.EditMessageText(editCtx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      fmt.Sprintf("Converting to %s failed. Please try again later.", label),
	}); err != nil {
		c.Logger.Error().Stack().Err(err).Msg("edit conversion failure message")
	}
}
