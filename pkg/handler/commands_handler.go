package handler

import (
	"context"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg"
	"github.com/gythialy/magnet/pkg/entities"
	"gorm.io/gorm"
	"strings"
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
	db            *gorm.DB
	keywordDao    *entities.KeywordDao
	tenderCodeDao *entities.TenderCodeDao
}

func NewCommandsHandler(ctx *pkg.BotContext) *CommandsHandler {
	db := ctx.DB
	return &CommandsHandler{
		db:            db,
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
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Added: " + result,
	})
}

func (c *CommandsHandler) DeleteKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, DeleteKeyword))
	keywords := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.keywordDao.Delete(keywords, id)

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Deleted: " + result,
	})
}

func (c *CommandsHandler) ListKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	result := c.keywordDao.ListKeywords(id)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "All keywords: " + strings.Join(result, ", "),
	})
}

func (c *CommandsHandler) AddTenderCodeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, AddTenderCode))
	codes := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.tenderCodeDao.Add(codes, id)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Added: " + result,
	})
}

func (c *CommandsHandler) DeleteTenderCodeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, DeleteTenderCode))
	codes := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := c.tenderCodeDao.Delete(codes, id)

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Deleted: " + result,
	})
}

func (c *CommandsHandler) ListTenderCodeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	result := c.tenderCodeDao.ListTenderCodes(id)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "All Codes: " + strings.Join(result, ", "),
	})
}
