package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

const (
	historyPageSize  = 20
	alarmPageSize    = 20
	defaultMessageId = 0
	// callbackTemplate is the wire format of every paginated callback:
	// "<prefix><page>:<term>".
	callbackTemplate = "%s%d:%s"
)

var (
	codeRegx    = regexp.MustCompile(`(?:（|[（(])([0-9]{4}-[A-Z]+-[A-Z0-9]+)(?:）|[）)])`)
	breakerRegx = regexp.MustCompile(`[\n\t]+`)
	spaceRegx   = regexp.MustCompile(`\s+`)

	// titleFetchClient fetches a notice page to derive the output file name.
	titleFetchClient = &http.Client{Timeout: 10 * time.Second}
)

type CommandsHandler struct {
	ctx *BotContext
	// pendingEdits holds the ForceReply prompt /statistics is waiting for,
	// keyed by chat id.
	pendingEdits sync.Map
}

func NewCommandsHandler(ctx *BotContext) *CommandsHandler {
	return &CommandsHandler{ctx: ctx}
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
	result, err := dal.Keyword.DeleteByIds(update.Message.Chat.ID, tmp)
	if err != nil {
		c.sendErrorMessage(ctx, b, update, fmt.Sprintf("%s failed, %s", constant.DeleteKeyword, err.Error()))
		return
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("%s: %s", constant.DeleteKeyword, result),
	}); err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	}
}

func (c *CommandsHandler) EditKeywordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	tmp := strings.TrimSpace(strings.TrimPrefix(text, constant.EditKeyword))
	if tmp == "" {
		c.sendErrorMessage(
			ctx, b, update,
			fmt.Sprintf(`Invalid format. Please use the following format: %s id1="new_keyword1";id2=new_keyword2`, constant.EditKeyword),
		)
		return
	}

	if err := dal.Keyword.EditById(update.Message.Chat.ID, strings.Split(tmp, ";")); err != nil {
		c.sendErrorMessage(ctx, b, update, fmt.Sprintf("%s failed, %s", constant.EditKeyword, err.Error()))
		return
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("%s: successful.", constant.EditKeyword),
	}); err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("send edit keyword message failed")
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

	alarm, err := dal.Alarm.GetById(id, businessId)
	if err != nil {
		c.sendErrorMessage(ctx, b, update, err.Error())
		return
	}
	message, _ := alarm.ToMessage()
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      update.Message.Chat.ID,
		Text:        message,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: models.ReplyParameters{MessageID: update.Message.ID},
	}); err != nil {
		c.ctx.Logger.Error().Err(err).Msg("")
	}
}

func (c *CommandsHandler) SearchAlarmRecordHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	id := update.Message.Chat.ID
	term := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, constant.SearchAlarmRecords))
	c.paginatedAlarms(ctx, b, id, term, 1, defaultMessageId)
}

// page describes one rendered page of a paginated command.
type page struct {
	number     int
	totalPages int
}

// pageKeyboard returns the Previous/Next inline keyboard of a page, or nil
// when there is only one page.
func pageKeyboard(p page, prefix, term string) *models.InlineKeyboardMarkup {
	var row []models.InlineKeyboardButton
	if p.number > 1 {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("« Previous (%d)", p.number-1),
			CallbackData: fmt.Sprintf(callbackTemplate, prefix, p.number-1, term),
		})
	}
	if p.number < p.totalPages {
		row = append(row, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("Next (%d) »", p.number+1),
			CallbackData: fmt.Sprintf(callbackTemplate, prefix, p.number+1, term),
		})
	}
	if len(row) == 0 {
		return nil
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{row}}
}

// pageCount returns how many pages hold total rows.
func pageCount(total int64, size int) int {
	return int((total + int64(size) - 1) / int64(size))
}

// renderPage numbers the items of one page and joins them with newlines, so the
// three list commands only have to describe a single line.
func renderPage[T any](items []T, pageNumber, size int, line func(item T) string) string {
	var sb strings.Builder
	for i, item := range items {
		fmt.Fprintf(&sb, "%d. %s\n", (pageNumber-1)*size+i+1, line(item))
	}
	return sb.String()
}

func (c *CommandsHandler) paginatedAlarms(ctx context.Context, b *bot.Bot,
	userId int64, term string, pageNumber, messageID int,
) {
	alarms, total := dal.Alarm.SearchByName(userId, term, pageNumber, alarmPageSize)
	if total == 0 {
		c.sendOrEditMessage(ctx, b, userId, messageID, "No alarm records found.", nil)
		return
	}

	body := renderPage(alarms, pageNumber, alarmPageSize, func(alarm *model.Alarm) string {
		link := fmt.Sprintf("https://t.me/%s?start=alarm_%s", config.TelegramName(), alarm.BusinessID)
		return fmt.Sprintf(`<a href="%s">%s @%s</a>`, link, alarm.CreditName, alarm.StartDate.Format("2006-01-02"))
	})
	p := page{number: pageNumber, totalPages: pageCount(total, alarmPageSize)}
	c.sendOrEditMessage(ctx, b, userId, messageID, body, pageKeyboard(p, constant.AlarmCallback, term))
}

func (c *CommandsHandler) SearchHistoryHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := update.Message.Text
	term := strings.TrimSpace(strings.TrimPrefix(text, constant.SearchHistory))
	c.paginatedSearchResult(ctx, b, update.Message.Chat.ID, term, 1, defaultMessageId)
}

func (c *CommandsHandler) paginatedSearchResult(ctx context.Context, b *bot.Bot,
	chatID int64, term string, pageNumber, messageID int,
) {
	results, total := dal.History.SearchByTitle(chatID, term, pageNumber, historyPageSize)
	if total == 0 {
		c.sendOrEditMessage(ctx, b, chatID, messageID, "No matching history found.", nil)
		return
	}

	body := renderPage(results, pageNumber, historyPageSize, func(history *model.History) string {
		return fmt.Sprintf(`<a href="%s">%s</a> @ %s`, history.URL, history.Title,
			history.UpdatedAt.Format("2006-01-02 15:04:05"))
	})
	p := page{number: pageNumber, totalPages: pageCount(total, historyPageSize)}
	c.sendOrEditMessage(ctx, b, chatID, messageID, body, pageKeyboard(p, constant.SearchCallback, term))
}

func (c *CommandsHandler) HandleCallbackQuery(ctx context.Context, b *bot.Bot, update *models.Update) {
	query := update.CallbackQuery
	if query == nil {
		return
	}
	// Answer first so the client stops showing a spinner, even when the
	// payload below turns out to be unusable.
	defer func() {
		if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
		}); err != nil {
			c.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}()

	prefix, pageNumber, term, err := parseCallbackData(query.Data)
	if err != nil {
		c.ctx.Logger.Warn().Err(err).Str("data", query.Data).Msg("ignoring callback")
		return
	}
	// The message can be inaccessible once it is too old or was deleted; there
	// is nothing to edit in that case.
	if query.Message.Message == nil {
		c.ctx.Logger.Debug().Msg("callback refers to an inaccessible message")
		return
	}
	messageID := query.Message.Message.ID
	chatID := query.Message.Message.Chat.ID

	switch prefix + ":" {
	case constant.SearchCallback:
		c.paginatedSearchResult(ctx, b, chatID, term, pageNumber, messageID)
	case constant.AlarmCallback:
		c.paginatedAlarms(ctx, b, query.From.ID, term, pageNumber, messageID)
	case constant.TodayCallback:
		c.paginatedTodayResult(ctx, b, chatID, pageNumber, messageID)
	}
}

// errMalformedCallback reports callback data that does not match
// "<prefix>:<page>:<term>".
var errMalformedCallback = errors.New("malformed callback data")

// parseCallbackData splits the callback wire format into its parts. Telegram
// relays whatever a client sends, so every part is validated here instead of
// being indexed blindly.
func parseCallbackData(data string) (prefix string, page int, term string, err error) {
	prefix, rest, ok := strings.Cut(data, ":")
	if !ok {
		return "", 0, "", errMalformedCallback
	}
	pageText, term, ok := strings.Cut(rest, ":")
	if !ok {
		return "", 0, "", errMalformedCallback
	}
	page, err = strconv.Atoi(pageText)
	if err != nil || page < 1 {
		return "", 0, "", fmt.Errorf("invalid callback page %q", pageText)
	}
	return prefix, page, term, nil
}

func (c *CommandsHandler) ConvertURLToPDFHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.convertURL(ctx, b, update, constant.ConvertPDF, model.PDF)
}

func (c *CommandsHandler) ConvertURLToIMGHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.convertURL(ctx, b, update, constant.ConvertIMG, model.IMG)
}

// convertURL starts one conversion for a chat command. The shared service owns
// the placeholder message, the file name and the Gotenberg submission, so the
// command and POST /api/convert behave identically.
func (c *CommandsHandler) convertURL(ctx context.Context, b *bot.Bot, update *models.Update,
	cmd string, kind model.FileType,
) {
	userID := update.Message.Chat.ID

	msgId, parsedURL, err := extractURL(update, cmd)
	if err != nil {
		c.sendErrorMessage(ctx, b, update, "Invalid URL format")
		return
	}

	if _, err := c.ctx.StartConversion(ctx, userID, parsedURL.String(), kind, msgId); err != nil {
		// A rejected host is the one failure the user can fix with another
		// link. Later failures already replaced the placeholder with a notice,
		// so they are only logged here.
		if errors.Is(err, ErrDomainNotAllowed) {
			c.sendErrorMessage(ctx, b, update, "URL domain is not allowed")
			return
		}
		c.ctx.Logger.Error().Stack().Err(err).Msg("start conversion")
	}
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
		return
	}

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

// extractFileName derives the output file name from the notice page title,
// falling back to the last path segment. It is a free function because the
// conversion service uses it as well, and the two callers must agree on the
// produced name.
func extractFileName(ctx context.Context, u *url.URL) (string, error) {
	urlFileName := path.Base(u.Path)
	if urlFileName == "/" {
		urlFileName = ""
	} else {
		urlFileName = strings.TrimSuffix(urlFileName, ".html")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := titleFetchClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	title := strings.TrimSpace(doc.Find("h1.info-title").Text())
	title = breakerRegx.ReplaceAllString(title, "")
	title = spaceRegx.ReplaceAllString(title, "")
	if title == "" {
		return urlFileName, nil
	}

	if matches := codeRegx.FindStringSubmatch(title); len(matches) > 1 {
		return matches[1], nil
	}
	return title, nil
}

func extractURL(update *models.Update, cmd string) (int, *url.URL, error) {
	message := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, cmd))
	m := update.Message.ReplyToMessage

	if m != nil {
		link := ""
		for _, entity := range update.Message.ReplyToMessage.Entities {
			if entity.Type == models.MessageEntityTypeTextLink && entity.URL != "" {
				link = entity.URL
				break
			}
		}
		parsed, err := url.Parse(link)
		return m.ID, parsed, err
	}

	parsed, err := url.Parse(message)
	return update.Message.ID, parsed, err
}

func (c *CommandsHandler) ListTodayHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.paginatedTodayResult(ctx, b, update.Message.Chat.ID, 1, defaultMessageId)
}

func (c *CommandsHandler) paginatedTodayResult(ctx context.Context, b *bot.Bot,
	chatID int64, pageNumber, messageID int,
) {
	results, total := dal.History.GetTodayByUserId(chatID, pageNumber, historyPageSize)
	if total == 0 {
		c.sendOrEditMessage(ctx, b, chatID, messageID, "No records found for today.", nil)
		return
	}

	body := renderPage(results, pageNumber, historyPageSize, func(history *model.History) string {
		fireEmoji := ""
		if history.HasTenderCode == 1 {
			fireEmoji = "🔥"
		}
		return fmt.Sprintf(`<a href="%s">%s%s</a> @ %s`, history.URL, fireEmoji, history.Title,
			history.UpdatedAt.Format("2006-01-02 15:04:05"))
	})
	p := page{number: pageNumber, totalPages: pageCount(total, historyPageSize)}
	c.sendOrEditMessage(ctx, b, chatID, messageID, body, pageKeyboard(p, constant.TodayCallback, ""))
}
