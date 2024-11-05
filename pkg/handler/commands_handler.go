package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/gythialy/magnet/pkg/utils"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	historyPageSize  = 20
	alarmPageSize    = 5
	defaultMessageId = 0
)

var (
	codeRegx    = regexp.MustCompile(`(?:（|[（(])([0-9]{4}-[A-Z]+-[A-Z0-9]+)(?:）|[）)])`)
	breakerRegx = regexp.MustCompile(`[\n\t]+`)
	spaceRegx   = regexp.MustCompile(`\s+`)
)

type CommandsHandler struct {
	ctx *BotContext
}

func NewCommandsHandler(ctx *BotContext) *CommandsHandler {
	return &CommandsHandler{
		ctx,
	}
}

func (c *CommandsHandler) addKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update, prefix string, t model.KeywordType) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, prefix))
	keywords := strings.Split(tmp, ",")
	id := update.Message.Chat.ID
	result := dal.Keyword.Insert(keywords, id, t)
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("%s: %s", prefix, result),
	}); err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
	}
}

func (c *CommandsHandler) AddKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.addKeywordHandler(ctx, b, update, constant.AddKeyword, model.PROJECT)
}

func (c *CommandsHandler) DeleteKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, constant.DeleteKeyword))
	if result, err := dal.Keyword.DeleteByIds(tmp); err == nil {
		if _, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   fmt.Sprintf("%s: %s", constant.DeleteKeyword, result),
		}); msgErr != nil {
			c.ctx.Logger.Error().Err(msgErr)
		}
	} else {
		if _, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   fmt.Sprintf("%s failed, %s", constant.DeleteKeyword, err.Error()),
		}); msgErr != nil {
			c.ctx.Logger.Error().Err(msgErr)
		}
	}
}

func (c *CommandsHandler) EditKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, constant.EditKeyword))
	split := strings.Split(tmp, ",")
	if len(split) != 2 {
		c.sendErrorMessage(ctx, b, update,
			fmt.Sprintf(`Invalid format. Please use the following format: %s keyword1="new_keyword1",keyword2=new_keyword2`, constant.EditKeyword),
		)
		return
	}
	if err := dal.Keyword.EditById(split); err == nil {
		if _, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   fmt.Sprintf("%s: successful.", constant.DeleteKeyword),
		}); msgErr != nil {
			c.ctx.Logger.Error().Err(msgErr)
		}
	} else {
		if _, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   fmt.Sprintf("%s failed, %s", constant.DeleteKeyword, err.Error()),
		}); msgErr != nil {
			c.ctx.Logger.Error().Err(msgErr)
		}
	}
}

func (c *CommandsHandler) AddAlarmKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.addKeywordHandler(ctx, b, update, constant.AddAlarmKeyword, model.ALARM)
}

func (c *CommandsHandler) ListAlarmRecordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	c.paginatedAlarms(ctx, b, id, 1, defaultMessageId)
}

func (c *CommandsHandler) paginatedAlarms(ctx context.Context, b *bot.Bot,
	userId int64, page, messageId int,
) {
	pageSize := alarmPageSize
	alarms, total := dal.Alarm.List(userId, page, pageSize)

	if total == 0 {
		text := "No alarm records found."
		c.sendOrEditMessage(ctx, b, userId, messageId, text, nil)
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize

	var response strings.Builder
	for i, alarm := range alarms {
		if markdown, err := alarm.ToTelegramMessage(); err == nil {
			response.WriteString(fmt.Sprintf("%d. %s\n", (page-1)*pageSize+i+1, markdown))
			response.WriteString("\n")
		} else {
			c.ctx.Logger.Error().Msgf("%s,%s", utils.ToString(alarm), err.Error())
		}
	}

	var keyboard [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton
	if page > 1 {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("« Previous (%d)", page-1),
			CallbackData: fmt.Sprintf("%s%d", constant.Alarm, page-1),
		})
	}

	if page < totalPages {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("Next (%d) »", page+1),
			CallbackData: fmt.Sprintf("%s%d", constant.Alarm, page+1),
		})
	}

	if len(row) > 0 {
		keyboard = append(keyboard, row)
	}

	replyMarkup := models.InlineKeyboardMarkup{
		InlineKeyboard: keyboard,
	}

	c.sendOrEditMessage(ctx, b, userId, messageId, response.String(), &replyMarkup)
}

func (c *CommandsHandler) SearchHistoryHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	query := strings.TrimSpace(strings.TrimPrefix(text, constant.SearchHistory))
	id := update.Message.Chat.ID

	if query == "" {
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: id,
			Text:   "Please provide a search term",
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
		return
	}

	// Get first page of results
	c.paginatedSearchResult(ctx, b, update, query, 1, defaultMessageId)
}

func (c *CommandsHandler) paginatedSearchResult(ctx context.Context, b *bot.Bot, update *models.Update,
	query string, page, messageId int,
) {
	id := update.Message.Chat.ID
	results, total := dal.History.SearchByTitle(id, query, page, historyPageSize)

	if total == 0 {
		text := "No matching history found."
		c.sendOrEditMessage(ctx, b, id, messageId, text, nil)
		return
	}

	totalPages := (int(total) + historyPageSize - 1) / historyPageSize

	var response strings.Builder
	for i, history := range results {
		response.WriteString(fmt.Sprintf("%d. <a href=\"%s\">%s</a>\n", (page-1)*historyPageSize+i+1, history.URL, history.Title))
	}

	var keyboard [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton

	if page > 1 {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("« Previous (%d)", page-1),
			CallbackData: fmt.Sprintf("%s%d:%s", constant.Search, page-1, query),
		})
	}

	if page < totalPages {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("Next (%d) »", page+1),
			CallbackData: fmt.Sprintf("%s%d:%s", constant.Search, page+1, query),
		})
	}

	if len(row) > 0 {
		keyboard = append(keyboard, row)
	}

	replyMarkup := models.InlineKeyboardMarkup{
		InlineKeyboard: keyboard,
	}

	c.sendOrEditMessage(ctx, b, id, messageId, response.String(), &replyMarkup)
}

func (c *CommandsHandler) HandleCallbackQuery(ctx context.Context, b *bot.Bot, update *models.Update) {
	data := update.CallbackQuery.Data
	parts := strings.Split(data, ":")
	if len(parts) < 1 {
		return
	}

	queryType := parts[0]
	page, err := strconv.Atoi(parts[1])
	if err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
		return
	}

	messageId := update.CallbackQuery.Message.Message.ID
	switch {
	case strings.HasPrefix(constant.Search, queryType):
		query := parts[2]
		c.paginatedSearchResult(ctx, b, &models.Update{
			Message: update.CallbackQuery.Message.Message,
		}, query, page, messageId)
	case strings.HasPrefix(constant.Alarm, queryType):
		c.paginatedAlarms(ctx, b, update.CallbackQuery.From.ID, page, messageId)
	}

	// Answer the callback query to remove the loading indicator
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	}); err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
	}
}

func (c *CommandsHandler) ConvertURLToPDFHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID

	parsedURL, urlErr := extractURL(update, constant.ConvertPDF)
	if urlErr != nil {
		c.sendErrorMessage(ctx, b, update, "Invalid URL format")
		return
	}

	// Check if the domain matches BotContext.MessageServerUrl
	if parsedURL.Host != c.ctx.Config.MessageServerUrl {
		c.sendErrorMessage(ctx, b, update, "URL domain is not allowed")
		return
	}

	// Generate a unique identifier for this request
	fileName := ""
	if f, err := c.extractFileName(parsedURL); err == nil {
		fileName = f + constant.PDFExtension
	} else {
		c.ctx.Logger.Error().Msg(err.Error())
	}

	// Send processing message
	processingMsg, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: userId,
		Text:   fmt.Sprintf("Converting URL to PDF(%s). Please wait...⌛", fileName),
	})
	if msgErr != nil {
		c.ctx.Logger.Error().Msg(msgErr.Error())
		return
	}

	go func() {
		u := parsedURL.String()
		if requestId, err := c.ctx.GotenbergClient.URLToPDF(u); err == nil {
			c.ctx.Store.Set(requestId, model.RequestInfo{
				ChatId:         userId,
				MessageId:      processingMsg.ID,
				Message:        u,
				ReplyMessageId: update.Message.ID,
				FileName:       fileName,
				Type:           model.PDF,
			}, DefaultCacheDuration)
		} else {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}()
}

func (c *CommandsHandler) ConvertURLToIMGHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID

	parsedURL, urlErr := extractURL(update, constant.ConvertIMG)
	if urlErr != nil {
		c.sendErrorMessage(ctx, b, update, "Invalid URL format")
		return
	}

	// Check if the domain matches BotContext.MessageServerUrl
	if parsedURL.Host != c.ctx.Config.MessageServerUrl {
		c.sendErrorMessage(ctx, b, update, "URL domain is not allowed")
		return
	}

	fileName := ""
	if f, err := c.extractFileName(parsedURL); err == nil {
		fileName = f + constant.ImgExtension
	} else {
		c.ctx.Logger.Error().Msg(err.Error())
	}

	// Send processing message
	processingMsg, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: userId,
		Text:   fmt.Sprintf("Converting URL to IMG(%s). Please wait...⌛", fileName),
	})
	if msgErr != nil {
		c.ctx.Logger.Error().Msg(msgErr.Error())
		return
	}

	go func() {
		u := parsedURL.String()
		if requestId, err := c.ctx.GotenbergClient.URLToImage(u); err == nil {
			c.ctx.Store.Set(requestId, model.RequestInfo{
				ChatId:         userId,
				MessageId:      processingMsg.ID,
				ReplyMessageId: update.Message.ID,
				Message:        u,
				FileName:       fileName,
				Type:           model.IMG,
			}, DefaultCacheDuration)
		} else {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}()
}

func (c *CommandsHandler) sendOrEditMessage(ctx context.Context, b *bot.Bot, chatID int64, messageID int, text string, replyMarkup *models.InlineKeyboardMarkup) {
	if messageID == 0 {
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: replyMarkup,
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	} else {
		if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   messageID,
			Text:        text,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: replyMarkup,
		}); err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}
}

func (c *CommandsHandler) sendErrorMessage(ctx context.Context, b *bot.Bot, update *models.Update, errorMsg string) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   "Error: " + errorMsg,
		ReplyParameters: &models.ReplyParameters{
			MessageID: update.Message.ID,
		},
	}); err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
	}
}

func (c *CommandsHandler) StaticHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID

	// Get counter
	keywordDao := dal.Keyword
	alarmCount := keywordDao.CountByUserId(userId, model.ALARM)
	alarmKeywords := keywordDao.GetByUserIdAndType(userId, model.ALARM)
	keywordCount := keywordDao.CountByUserId(userId, model.PROJECT)
	historyCount := dal.History.CountByUserId(userId)
	var alarmStats strings.Builder
	if len(alarmKeywords) > 0 {
		alarmStats.WriteString(fmt.Sprintf("\n- Alarm Keywords: %d\n", alarmCount))
		for idx, kw := range alarmKeywords {
			alarmStats.WriteString(fmt.Sprintf("\n- [%d/%d] %s", idx+1, *kw.ID, kw.Keyword))
		}
	}
	// Get keyword stats
	keywords := keywordDao.GetByUserIdAndType(userId, model.PROJECT)
	var keywordStats strings.Builder
	if len(keywords) > 0 {
		keywordStats.WriteString(fmt.Sprintf("\n- Keyword Match Counts: %d\n", keywordCount))
		for idx, kw := range keywords {
			if cr := rule.NewComplexRule(kw); cr != nil {
				keywordStats.WriteString(fmt.Sprintf("\n- [%d/%d] %s => [%s]: %d", idx+1, *kw.ID, kw.Keyword, cr.ToString(), kw.Counter))
			} else {
				keywordStats.WriteString(fmt.Sprintf("\n- [%d/%d] %s: %d", idx+1, *kw.ID, kw.Keyword, kw.Counter))
			}
		}
	}

	responseText := fmt.Sprintf(`<b>About Magnet Bot</b>
Version: %s
Build Time: %s

<b>Statistics:</b>
- History Records: %d
%s
%s
`,
		constant.Version,
		constant.BuildTime,
		historyCount,
		alarmStats.String(),
		keywordStats.String())

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		Text:      responseText,
		ParseMode: models.ParseModeHTML,
	}); err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
	}
}

func (c *CommandsHandler) extractFileName(u *url.URL) (string, error) {
	urlPath := u.Path
	urlFileName := path.Base(urlPath)
	if urlFileName == "/" {
		urlFileName = ""
	} else {
		urlFileName = strings.TrimSuffix(urlFileName, ".html")
	}

	// Alarms the page to get the title
	resp, err := http.Get(u.String())
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}(resp.Body)

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	var fileName string
	title := strings.TrimSpace(doc.Find("h1.info-title").Text())
	title = breakerRegx.ReplaceAllString(title, "")
	title = spaceRegx.ReplaceAllString(title, "")

	if title != "" {
		matches := codeRegx.FindStringSubmatch(title)
		if len(matches) > 1 {
			// Use the extracted code as the filename
			fileName = matches[1]
		} else {
			// If no code found, use the full cleaned title
			fileName = title
		}
	} else {
		// If title not found, use the URL filename
		fileName = urlFileName
	}
	return fileName, nil
}

func extractURL(update *models.Update, cmd string) (*url.URL, error) {
	message := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, cmd))
	u := ""
	if message == "" {
		entities := update.Message.ReplyToMessage.Entities
		for _, entity := range entities {
			if entity.Type == models.MessageEntityTypeTextLink && entity.URL != "" {
				u = entity.URL
				break
			}
		}
	}

	return url.Parse(u)
}
