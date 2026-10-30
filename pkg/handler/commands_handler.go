package handler

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"

	"golang.org/x/text/width"
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
	c.convertURL(ctx, b, update, constant.ConvertPDF, model.PDF, constant.PDFExtension, "PDF")
}

func (c *CommandsHandler) ConvertURLToIMGHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	c.convertURL(ctx, b, update, constant.ConvertIMG, model.IMG, constant.ImgExtension, "IMG")
}

// convertURL starts one Gotenberg conversion: the file itself arrives later on
// the webhook, keyed by the request id stored here.
func (c *CommandsHandler) convertURL(ctx context.Context, b *bot.Bot, update *models.Update,
	cmd string, kind model.FileType, extension, label string,
) {
	userID := update.Message.Chat.ID

	msgId, parsedURL, err := extractURL(update, cmd)
	if err != nil {
		c.sendErrorMessage(ctx, b, update, "Invalid URL format")
		return
	}
	// Only notices from the configured server may be converted.
	if parsedURL.Host != c.ctx.Config.MessageServerUrl {
		c.sendErrorMessage(ctx, b, update, "URL domain is not allowed")
		return
	}

	fileName := ""
	if f, ferr := c.extractFileName(ctx, parsedURL); ferr == nil {
		fileName = f + extension
	} else {
		c.ctx.Logger.Error().Stack().Err(ferr).Msg("")
	}

	processingMsg, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: userID,
		Text:   fmt.Sprintf("Converting URL to %s(%s). Please wait...⌛", label, fileName),
	})
	if err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
		return
	}

	request := model.RequestInfo{
		ChatId:         userID,
		MessageId:      processingMsg.ID,
		Message:        parsedURL.String(),
		ReplyMessageId: msgId,
		FileName:       fileName,
		Type:           kind,
	}
	go func() {
		var (
			requestID string
			convErr   error
		)
		switch kind {
		case model.PDF:
			requestID, convErr = c.ctx.Gotenberg.URLToPDF(c.ctx.context(), request.Message)
		case model.IMG:
			requestID, convErr = c.ctx.Gotenberg.URLToImage(c.ctx.context(), request.Message)
		}
		if convErr != nil {
			c.ctx.Logger.Error().Stack().Err(convErr).Msg("")
			return
		}
		c.ctx.Store.Set(requestID, request, DefaultCacheDuration)
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

func (c *CommandsHandler) StaticHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.Chat.ID

	responseText := renderStatistics(statisticsView{
		Version:   constant.Version,
		BuildTime: constant.BuildTime,
		History:   dal.History.CountByUserId(userId),
		Keywords:  dal.Keyword.GetByUserIdAndType(userId, model.PROJECT),
		Alarms:    dal.Keyword.GetByUserIdAndType(userId, model.ALARM),
	})

	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		Text:      responseText,
		ParseMode: models.ParseModeHTML,
	}); err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	}
}

const (
	// statisticsKeywordLimit and statisticsAlarmLimit keep the message short
	// enough for Telegram (4096 units) even for a user with many keywords.
	// Keywords without a single notice are listed sparingly: their ids are
	// useful for /edit_keywords, but they must not crowd out the ones that do
	// match.
	statisticsKeywordLimit     = 15
	statisticsZeroKeywordLimit = 5
	statisticsAlarmLimit       = 10

	// tableCellLimit caps one table cell's display width. Telegram wraps a
	// <pre> block at the bubble width, so a wider table would lose the
	// alignment that makes it a table in the first place.
	tableCellLimit = 22
)

// tableColumn describes one column of an asciiTable.
type tableColumn struct {
	title string
	right bool // right-align the values (numbers)
}

// asciiTable renders a bordered table inside a Telegram <pre> block.
//
// Cells are measured by display width, not byte or rune count: a CJK keyword
// occupies two monospace columns, so measuring it as one would break the
// borders. Long cells are truncated with an ellipsis to keep the table
// readable.
func asciiTable(columns []tableColumn, rows [][]string) string {
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = displayWidth(column.title)
	}
	for _, row := range rows {
		for i := range columns {
			if i < len(row) {
				widths[i] = max(widths[i], min(displayWidth(row[i]), tableCellLimit))
			}
		}
	}

	border := func() string {
		var b strings.Builder
		b.WriteByte('+')
		for _, w := range widths {
			b.WriteString(strings.Repeat("-", w+2))
			b.WriteByte('+')
		}
		return b.String()
	}
	line := func(cells []string) string {
		var b strings.Builder
		b.WriteByte('|')
		for i := range columns {
			cell := ""
			if i < len(cells) {
				cell = truncateToWidth(cells[i], widths[i])
			}
			padding := widths[i] - displayWidth(cell)

			b.WriteByte(' ')
			if columns[i].right {
				b.WriteString(strings.Repeat(" ", padding))
				b.WriteString(cell)
			} else {
				b.WriteString(cell)
				b.WriteString(strings.Repeat(" ", padding))
			}
			b.WriteString(" |")
		}
		return b.String()
	}

	lines := make([]string, 0, len(rows)+3)
	lines = append(lines, border(), line(columnTitles(columns)), border())
	for _, row := range rows {
		lines = append(lines, line(row))
	}
	lines = append(lines, border())
	return strings.Join(lines, "\n")
}

// columnTitles extracts the header cells of columns.
func columnTitles(columns []tableColumn) []string {
	titles := make([]string, len(columns))
	for i, column := range columns {
		titles[i] = column.title
	}
	return titles
}

// truncateToWidth shortens s to at most limit display columns.
func truncateToWidth(s string, limit int) string {
	if displayWidth(s) <= limit {
		return s
	}

	var b strings.Builder
	used := 0
	for _, r := range s {
		w := runeWidth(r)
		if used+w > limit-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	b.WriteString("…")
	return b.String()
}

// displayWidth returns how many monospace columns s occupies.
func displayWidth(s string) int {
	total := 0
	for _, r := range s {
		total += runeWidth(r)
	}
	return total
}

// runeWidth is the number of columns one rune occupies: East Asian wide and
// fullwidth runes (CJK, emoji) take two, everything else one.
func runeWidth(r rune) int {
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// statisticsView is the data /statistics renders.
type statisticsView struct {
	Version   string
	BuildTime string
	History   int64
	Keywords  []*model.Keyword
	Alarms    []*model.Keyword
}

// renderStatistics builds the /statistics message.
//
// The per-keyword number is the count of related notices, so the keywords are
// listed as a leaderboard (highest first) instead of insertion order, and every
// keyword text is escaped: it is user input rendered inside an HTML message.
func renderStatistics(v statisticsView) string {
	var b strings.Builder

	fmt.Fprintf(&b, "<b>magnet-bot</b> <code>%s</code>\n", html.EscapeString(v.Version))
	fmt.Fprintf(&b, "Build <code>%s</code>\n\n", html.EscapeString(v.BuildTime))

	var related int64
	for _, kw := range v.Keywords {
		related += int64(kw.Counter)
	}

	b.WriteString("<b>Statistics</b>\n")
	b.WriteString(renderTable(asciiTable(
		[]tableColumn{{title: "Metric"}, {title: "Value", right: true}},
		[][]string{
			{"History", groupDigits(v.History)},
			{"Keywords", strconv.Itoa(len(v.Keywords))},
			{"Related notices", groupDigits(related)},
			{"Alarm keywords", strconv.Itoa(len(v.Alarms))},
		},
	)))

	if len(v.Keywords) == 0 {
		b.WriteString("\nUse /add_keywords to subscribe to a keyword.\n")
	} else {
		keywords := slices.Clone(v.Keywords)
		slices.SortStableFunc(keywords, func(a, c *model.Keyword) int {
			if a.Counter != c.Counter {
				return cmp.Compare(c.Counter, a.Counter)
			}
			return cmp.Compare(keywordID(a), keywordID(c))
		})

		rows := make([][]string, 0, statisticsKeywordLimit)
		zeros := 0
		for _, kw := range keywords {
			if len(rows) == statisticsKeywordLimit {
				break
			}
			if kw.Counter == 0 {
				if zeros == statisticsZeroKeywordLimit {
					continue
				}
				zeros++
			}
			rows = append(rows, []string{
				groupDigits(int64(kw.Counter)),
				fmt.Sprintf("#%d", keywordID(kw)),
				kw.Keyword,
			})
		}

		b.WriteString("\n<b>Related notices per keyword</b>\n")
		b.WriteString(renderTable(asciiTable(
			[]tableColumn{{title: "Notices", right: true}, {title: "ID"}, {title: "Keyword"}},
			rows,
		)))
		if remaining := len(keywords) - len(rows); remaining > 0 {
			fmt.Fprintf(&b, "…and %d more\n", remaining)
		}
	}

	if len(v.Alarms) > 0 {
		alarmRows := make([][]string, 0, statisticsAlarmLimit)
		for _, kw := range v.Alarms {
			if len(alarmRows) == statisticsAlarmLimit {
				break
			}
			alarmRows = append(alarmRows, []string{fmt.Sprintf("#%d", keywordID(kw)), kw.Keyword})
		}

		b.WriteString("\n<b>Alarm keywords</b>\n")
		b.WriteString(renderTable(asciiTable(
			[]tableColumn{{title: "ID"}, {title: "Keyword"}},
			alarmRows,
		)))
		if remaining := len(v.Alarms) - len(alarmRows); remaining > 0 {
			fmt.Fprintf(&b, "…and %d more\n", remaining)
		}
	}

	return strings.TrimSpace(b.String())
}

// renderTable wraps a preformatted table in a <pre> block. The table is escaped
// as a whole: the borders are plain ASCII and survive escaping, while a keyword
// containing "<" or "&" cannot break the message.
func renderTable(table string) string {
	return "<pre>" + html.EscapeString(table) + "</pre>\n"
}

// keywordID dereferences the primary key, tolerating a nil pointer.
func keywordID(kw *model.Keyword) int32 {
	if kw.ID == nil {
		return 0
	}
	return *kw.ID
}

// groupDigits renders 12853 as 12,853.
func groupDigits(n int64) string {
	digits := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}

	var b strings.Builder
	b.WriteString(sign)
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// extractFileName derives the output file name from the notice page title,
// falling back to the last path segment.
func (c *CommandsHandler) extractFileName(ctx context.Context, u *url.URL) (string, error) {
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
