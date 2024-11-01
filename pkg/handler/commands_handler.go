package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gythialy/magnet/pkg/utils"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/PuerkitoBio/goquery"

	"github.com/google/uuid"
	"github.com/gythialy/magnet/pkg/rule"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	historyPageSize  = 20
	alarmPageSize    = 5
	defaultMessageId = 0
	fileExtension    = ".pdf"
)

var (
	codeRegx    = regexp.MustCompile(`[（(]([^）)]+)[）)]`)
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

	c.sendPaginatedAlarms(ctx, b, id, 1, defaultMessageId)
}

func (c *CommandsHandler) sendPaginatedAlarms(ctx context.Context, b *bot.Bot,
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
			CallbackData: fmt.Sprintf("%s:%d:%s", constant.Search, page-1, query),
		})
	}

	if page < totalPages {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("Next (%d) »", page+1),
			CallbackData: fmt.Sprintf("%s:%d:%s", constant.Search, page+1, query),
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
	switch queryType {
	case constant.Search:
		query := parts[2]
		c.paginatedSearchResult(ctx, b, &models.Update{
			Message: update.CallbackQuery.Message.Message,
		}, query, page, messageId)
	case constant.Alarm:
		c.sendPaginatedAlarms(ctx, b, update.CallbackQuery.From.ID, page, messageId)
	}

	// Answer the callback query to remove the loading indicator
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	}); err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
	}
}

func (c *CommandsHandler) ConvertURLToPDFHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	message := strings.TrimSpace(strings.TrimPrefix(text, constant.ConvertPDF))
	userId := update.Message.Chat.ID

	if message == "" {
		c.sendErrorMessage(ctx, b, update, "Please provide a URL to convert")
		return
	}

	parsedURL, err := url.Parse(message)
	if err != nil {
		c.sendErrorMessage(ctx, b, update, "Invalid URL format")
		return
	}

	// Check if the domain matches BotContext.MessageServerUrl
	if parsedURL.Host != c.ctx.Config.MessageServerUrl {
		c.sendErrorMessage(ctx, b, update, "URL domain is not allowed")
		return
	}

	// Generate a unique identifier for this request
	requestId := uuid.New().String()
	fileName := requestId + fileExtension
	if f, err := c.extractFileName(parsedURL); err == nil {
		fileName = f
	} else {
		c.ctx.Logger.Error().Msg(err.Error())
	}

	// Send processing message
	processingMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: userId,
		Text:   fmt.Sprintf("Converting URL to PDF(%s). Please wait...⌛", fileName),
	})
	if err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
		return
	}

	// Store the chat ID and file name associated with this request
	c.ctx.Store.Set(requestId, model.RequestInfo{
		ChatId:    userId,
		MessageId: processingMsg.ID,
		Message:   message,
		FileName:  fileName,
	}, 10*time.Minute)

	// Call Gotenberg service
	go c.pdfService(message, requestId)
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
			fileName = matches[1] + fileExtension
		} else {
			// If no code found, use the full cleaned title
			fileName = title + fileExtension
		}
	} else {
		// If title not found, use the URL filename
		fileName = urlFileName + fileExtension
	}
	return fileName, nil
}

func (c *CommandsHandler) pdfService(u string, requestID string) {
	webhookURL := fmt.Sprintf("%s%s%s", c.ctx.Config.PDF.WebhookURL(), constant.PDFEndPoint, requestID)

	// Create a new form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the URL field
	_ = writer.WriteField("url", u)

	// Close the multipart writer
	err := writer.Close()
	if err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
		return
	}

	// Create the request
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/forms/chromium/convert/url", c.ctx.Config.PDF.PDFServiceURL), body)
	if err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
		return
	}

	// Set headers
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Gotenberg-Webhook-Url", webhookURL)
	req.Header.Set("Gotenberg-Webhook-Error-Url", webhookURL)
	req.Header.Set("Gotenberg-Webhook-Method", "POST")

	// Send the request
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		c.ctx.Logger.Error().Msg(err.Error())
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			c.ctx.Logger.Error().Msg(err.Error())
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusNoContent {
		c.ctx.Logger.Error().Msgf("Gotenberg service returned status: %d", resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		c.ctx.Logger.Error().Msgf("Response body: %s", string(body))
	}
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
