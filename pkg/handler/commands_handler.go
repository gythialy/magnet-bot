package handler

import (
	"context"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
	"github.com/gythialy/magnet/pkg/entities"
)

const (
	AddKeyword    = "/add_keywords"
	DeleteKeyword = "/delete_keywords"
	ListKeyword   = "/list_keywords"

	AddTenderCode    = "/add_tender_codes"
	DeleteTenderCode = "/delete_tender_codes"
	ListTenderCode   = "/list_tender_codes"
)

type CommandsHandler struct {
	ctx           *pkg.BotContext
	keywordDao    *entities.KeywordDao
	tenderCodeDao *entities.TenderCodeDao
}

func NewCommandsHandler(ctx *pkg.BotContext) *CommandsHandler {
	db := ctx.DB
	return &CommandsHandler{
		ctx:           ctx,
		keywordDao:    entities.NewKeywordDao(db),
		tenderCodeDao: entities.NewTenderCodeDao(db),
	}
}

func (c *CommandsHandler) AddKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, AddKeyword))
	keywords := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.keywordDao.Add(keywords, id)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Added: " + result,
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}

func (c *CommandsHandler) DeleteKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, DeleteKeyword))
	keywords := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.keywordDao.Delete(keywords, id)

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Deleted: " + result,
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}

func (c *CommandsHandler) ListKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	result := c.keywordDao.ListKeywords(id)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "All keywords: " + strings.Join(result, ", "),
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}

func (c *CommandsHandler) AddTenderCodeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, AddTenderCode))
	codes := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.tenderCodeDao.Add(codes, id)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Added: " + result,
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}

func (c *CommandsHandler) DeleteTenderCodeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, DeleteTenderCode))
	codes := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.tenderCodeDao.Delete(codes, id)

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Deleted: " + result,
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}

func (c *CommandsHandler) ListTenderCodeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	result := c.tenderCodeDao.ListTenderCodes(id)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "All Codes: " + strings.Join(result, ", "),
	}); err != nil {
		c.ctx.Logger.Error().Err(err)
	}
}
