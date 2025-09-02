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

	"github.com/gythialy/magnet/pkg/config"

	"github.com/PuerkitoBio/goquery"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"github.com/gythialy/magnet/pkg/rule"

	"github.com/gythialy/magnet/pkg/constant"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	historyPageSize  = 20
	alarmPageSize    = 20
	defaultMessageId = 0
	alarmTemplate    = "%s%d:%s"
	historyTemplate  = alarmTemplate
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
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
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
			c.ctx.Logger.Error().Stack().Err(msgErr).Msg("")
		}
	} else {
		if _, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   fmt.Sprintf("%s failed, %s", constant.DeleteKeyword, err.Error()),
		}); msgErr != nil {
			c.ctx.Logger.Error().Stack().Err(msgErr).Msg("")
		}
	}
}

func (c *CommandsHandler) EditKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, constant.EditKeyword))
	split := strings.Split(tmp, ";")
	if len(split) < 1 {
		c.sendErrorMessage(ctx, b, update,
			fmt.Sprintf(`Invalid format. Please use the following format: %s id1="new_keyword1";id2=new_keyword2`, constant.EditKeyword),
		)
		return
	}
	if err := dal.Keyword.EditById(split); err == nil {
		if _, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   fmt.Sprintf("%s: successful.", constant.EditKeyword),
		}); msgErr != nil {
			c.ctx.Logger.Error().Err(msgErr)
		}
	} else {
		if _, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   fmt.Sprintf("%s failed, %s", constant.EditKeyword, err.Error()),
		}); msgErr != nil {
			c.ctx.Logger.Error().Stack().Err(msgErr).Msg("")
		}
	}
}

func (c *CommandsHandler) AddAlarmKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.addKeywordHandler(ctx, b, update, constant.AddAlarmKeyword, model.ALARM)
}

func (c *CommandsHandler) AlarmRecordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	businessId := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, constant.Alarm))

	if businessId == "" {
		c.sendErrorMessage(ctx, b, update, "Please provide a valid alarm ID.")
		return
	}

	if alarm, err := dal.Alarm.GetById(id, businessId); err == nil {
		message, _ := alarm.ToMessage()
		if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      update.Message.Chat.ID,
			Text:        message,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: models.ReplyParameters{MessageID: update.Message.ID},
		}); err != nil {
			c.ctx.Logger.Error().Err(err).Msg("")
		}
	} else {
		c.sendErrorMessage(ctx, b, update, err.Error())
	}
}

func (c *CommandsHandler) SearchAlarmRecordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	term := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, constant.SearchAlarmRecords))
	c.paginatedAlarms(ctx, b, id, term, 1, defaultMessageId)
}

func (c *CommandsHandler) paginatedAlarms(ctx context.Context, b *bot.Bot,
	userId int64, term string, page, messageId int,
) {
	alarms, total := dal.Alarm.SearchByName(userId, term, page, alarmPageSize)

	if total == 0 {
		text := "No alarm records found."
		c.sendOrEditMessage(ctx, b, userId, messageId, text, nil)
		return
	}

	totalPages := (int(total) + alarmPageSize - 1) / alarmPageSize

	var response strings.Builder
	for i, alarm := range alarms {
		response.WriteString(fmt.Sprintf("%d. <a href=\"%s\">%s @%s</a>\n", (page-1)*alarmPageSize+i+1,
			fmt.Sprintf("https://t.me/%s?start=alarm_%s", config.TelegramName(), alarm.BusinessID),
			alarm.CreditName, alarm.StartDate.Format("2006-01-02")))
	}

	var keyboard [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton

	if page > 1 {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("« Previous (%d)", page-1),
			CallbackData: fmt.Sprintf(alarmTemplate, constant.AlarmCallback, page-1, term),
		})
	}

	if page < totalPages {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("Next (%d) »", page+1),
			CallbackData: fmt.Sprintf(alarmTemplate, constant.AlarmCallback, page+1, term),
		})
	}
	var replyMarkup *models.InlineKeyboardMarkup
	if len(row) > 0 {
		keyboard = append(keyboard, row)
		replyMarkup = &models.InlineKeyboardMarkup{
			InlineKeyboard: keyboard,
		}
	}

	c.sendOrEditMessage(ctx, b, userId, messageId, response.String(), replyMarkup)
}

func (c *CommandsHandler) SearchHistoryHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	term := strings.TrimSpace(strings.TrimPrefix(text, constant.SearchHistory))
	// Get the first page of results
	c.paginatedSearchResult(ctx, b, update, term, 1, defaultMessageId)
}

func (c *CommandsHandler) paginatedSearchResult(ctx context.Context, b *bot.Bot, update *models.Update,
	term string, page, messageId int,
) {
	id := update.Message.Chat.ID
	results, total := dal.History.SearchByTitle(id, term, page, historyPageSize)

	if total == 0 {
		text := "No matching history found."
		c.sendOrEditMessage(ctx, b, id, messageId, text, nil)
		return
	}

	totalPages := (int(total) + historyPageSize - 1) / historyPageSize

	var response strings.Builder
	for i, history := range results {
		response.WriteString(fmt.Sprintf("%d. <a href=\"%s\">%s</a> @ %s\n",
			(page-1)*historyPageSize+i+1,
			history.URL,
			history.Title,
			history.UpdatedAt.Format("2006-01-02 15:04:05")))
	}

	var keyboard [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton

	if page > 1 {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("« Previous (%d)", page-1),
			CallbackData: fmt.Sprintf(historyTemplate, constant.SearchCallback, page-1, term),
		})
	}

	if page < totalPages {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("Next (%d) »", page+1),
			CallbackData: fmt.Sprintf(historyTemplate, constant.SearchCallback, page+1, term),
		})
	}

	var replyMarkup *models.InlineKeyboardMarkup

	if len(row) > 0 {
		keyboard = append(keyboard, row)

		replyMarkup = &models.InlineKeyboardMarkup{
			InlineKeyboard: keyboard,
		}
	}

	c.sendOrEditMessage(ctx, b, id, messageId, response.String(), replyMarkup)
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
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
		return
	}

	messageId := update.CallbackQuery.Message.Message.ID
	switch {
	case strings.HasPrefix(constant.SearchCallback, queryType):
		term := parts[2]
		c.paginatedSearchResult(ctx, b, &models.Update{
			Message: update.CallbackQuery.Message.Message,
		}, term, page, messageId)
	case strings.HasPrefix(constant.AlarmCallback, queryType):
		term := parts[2]
		c.paginatedAlarms(ctx, b, update.CallbackQuery.From.ID, term, page, messageId)
	case strings.HasPrefix(constant.TodayCallback, queryType):
		c.paginatedTodayResult(ctx, b, &models.Update{
			Message: update.CallbackQuery.Message.Message,
		}, page, messageId)
	}

	// Answer the callback query to remove the loading indicator
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	}); err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	}
}

func (c *CommandsHandler) ConvertURLToPDFHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID

	msgId, parsedURL, urlErr := extractURL(update, constant.ConvertPDF)
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
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	}

	// Send the processing message
	processingMsg, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: userId,
		Text:   fmt.Sprintf("Converting URL to PDF(%s). Please wait...⌛", fileName),
	})
	if msgErr != nil {
		c.ctx.Logger.Error().Stack().Err(msgErr).Msg("")
		return
	}

	go func() {
		u := parsedURL.String()
		if requestId, err := c.ctx.Gotenberg.URLToPDF(u); err == nil {
			c.ctx.Store.Set(requestId, model.RequestInfo{
				ChatId:         userId,
				MessageId:      processingMsg.ID,
				Message:        u,
				ReplyMessageId: msgId,
				FileName:       fileName,
				Type:           model.PDF,
			}, DefaultCacheDuration)
		} else {
			c.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}()
}

func (c *CommandsHandler) ConvertURLToIMGHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID

	msgId, parsedURL, urlErr := extractURL(update, constant.ConvertIMG)
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
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	}

	// Send a processing message
	processingMsg, msgErr := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: userId,
		Text:   fmt.Sprintf("Converting URL to IMG(%s). Please wait...⌛", fileName),
	})
	if msgErr != nil {
		c.ctx.Logger.Error().Stack().Err(msgErr).Msg("")
		return
	}

	go func() {
		u := parsedURL.String()
		if requestId, err := c.ctx.Gotenberg.URLToImage(u); err == nil {
			c.ctx.Store.Set(requestId, model.RequestInfo{
				ChatId:         userId,
				MessageId:      processingMsg.ID,
				ReplyMessageId: msgId,
				Message:        u,
				FileName:       fileName,
				Type:           model.IMG,
			}, DefaultCacheDuration)
		} else {
			c.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}()
}

func (c *CommandsHandler) sendOrEditMessage(ctx context.Context, b *bot.Bot, chatID int64, messageID int, text string, replyMarkup *models.InlineKeyboardMarkup) {
	if messageID == 0 {
		params := &bot.SendMessageParams{
			ChatID:    chatID,
			Text:      text,
			ParseMode: models.ParseModeHTML,
		}
		if replyMarkup != nil {
			params.ReplyMarkup = replyMarkup
		}
		if _, err := b.SendMessage(ctx, params); err != nil {
			c.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	} else {
		params := &bot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: messageID,
			Text:      text,
			ParseMode: models.ParseModeHTML,
		}
		if replyMarkup != nil {
			params.ReplyMarkup = replyMarkup
		}
		if _, err := b.EditMessageText(ctx, params); err != nil {
			c.ctx.Logger.Error().Stack().Err(err).Msg("")
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
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
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
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
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
			c.ctx.Logger.Error().Stack().Err(err).Msg("")
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
			fileName = matches[1]
		} else {
			fileName = title
		}
	} else {
		fileName = urlFileName
	}
	return fileName, nil
}

func extractURL(update *models.Update, cmd string) (int, *url.URL, error) {
	message := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, cmd))
	m := update.Message.ReplyToMessage

	if m != nil {
		entities := update.Message.ReplyToMessage.Entities
		u := ""
		for _, entity := range entities {
			if entity.Type == models.MessageEntityTypeTextLink && entity.URL != "" {
				u = entity.URL
				break
			}
		}
		parse, err := url.Parse(u)
		return m.ID, parse, err
	}

	parse, err := url.Parse(message)
	return update.Message.ID, parse, err
}

func (c *CommandsHandler) ListTodayHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Get the first page of today's results
	c.paginatedTodayResult(ctx, b, update, 1, defaultMessageId)
}

func (c *CommandsHandler) paginatedTodayResult(ctx context.Context, b *bot.Bot, update *models.Update,
	page, messageId int,
) {
	id := update.Message.Chat.ID
	results, total := dal.History.GetTodayByUserId(id, page, historyPageSize)

	if total == 0 {
		text := "No records found for today."
		c.sendOrEditMessage(ctx, b, id, messageId, text, nil)
		return
	}

	totalPages := (int(total) + historyPageSize - 1) / historyPageSize

	var response strings.Builder
	for i, history := range results {
		// 添加🔥标记如果HasTenderCode为1
		fireEmoji := ""
		if history.HasTenderCode == 1 {
			fireEmoji = "🔥"
		}

		response.WriteString(fmt.Sprintf("%d. <a href=\"%s\">%s%s</a> @ %s\n",
			(page-1)*historyPageSize+i+1,
			history.URL,
			fireEmoji,
			history.Title,
			history.UpdatedAt.Format("2006-01-02 15:04:05")))
	}

	var keyboard [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton

	if page > 1 {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("« Previous (%d)", page-1),
			CallbackData: fmt.Sprintf("today:%d:", page-1),
		})
	}

	if page < totalPages {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("Next (%d) »", page+1),
			CallbackData: fmt.Sprintf("today:%d:", page+1),
		})
	}

	var replyMarkup *models.InlineKeyboardMarkup

	if len(row) > 0 {
		keyboard = append(keyboard, row)

		replyMarkup = &models.InlineKeyboardMarkup{
			InlineKeyboard: keyboard,
		}
	}

	c.sendOrEditMessage(ctx, b, id, messageId, response.String(), replyMarkup)
}
