package handler

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
)

// The /statistics report is one message plus an inline keyboard: the counters on
// top, one page of keywords below, and one button per keyword. Every button
// carries the whole view state in its callback data, so paging and the detail
// screens need no server-side session.
//
// The report is plain text with one entry per line instead of an ASCII table: a
// table only lines up inside a <pre> block, and a <pre> block wider than the
// chat bubble is clipped on a phone — which is how the previous layout lost the
// keyword column. Plain lines reflow, so every entry stays readable.
const (
	statisticsPageSize = 8

	// statisticsTextLimit bounds one entry of the message and
	// statisticsButtonLimit one button label. Telegram caps a button label at 64
	// characters; only a pathological keyword (a pasted paragraph) is shortened
	// at all.
	statisticsTextLimit   = 64
	statisticsButtonLimit = 24

	// pendingKeywordEditTTL bounds how long a ForceReply prompt stays armed: a
	// reply that arrives later is an ordinary message again.
	pendingKeywordEditTTL = 5 * time.Minute
)

// Actions of the "stats:<action>:<page>:<id>" callback wire format.
const (
	statisticsActionPage    = "page"   // keyword list page
	statisticsActionAlarms  = "alarms" // alarm keyword list
	statisticsActionAlarmPg = "apage"  // alarm keyword list page
	statisticsActionBack    = "back"   // back to the keyword list
	statisticsActionOpen    = "kw"     // keyword detail
	statisticsActionEdit    = "edit"   // rename prompt
	statisticsActionDelete  = "del"    // delete confirmation
	statisticsActionDelOK   = "delok"  // confirmed delete
	statisticsActionCancel  = "cancel" // leave edit/delete, back to the detail
)

// statisticsKind is the screen one /statistics message currently shows.
type statisticsKind int

const (
	statisticsList statisticsKind = iota
	statisticsAlarmList
	statisticsDetail
	statisticsEdit
	statisticsDelete
)

var errMalformedStatisticsCallback = errors.New("malformed statistics callback data")

// statisticsView is the data /statistics renders.
type statisticsView struct {
	Version   string
	BuildTime string
	History   int64
	Keywords  []*model.Keyword
	Alarms    []*model.Keyword
}

// statisticsScreen is the view state of one /statistics message: the data, the
// screen kind, the list page (0-based) and the keyword a detail screen shows.
type statisticsScreen struct {
	view statisticsView
	kind statisticsKind
	page int
	id   int32
}

// pendingKeywordEdit is the reply /statistics waits for after the user tapped
// Edit: which keyword to rename and which message to refresh afterwards.
type pendingKeywordEdit struct {
	keywordID       int32
	chatID          int64
	messageID       int
	promptMessageID int
	expires         time.Time
}

// loadStatistics reads the counters /statistics shows for one chat.
func (c *CommandsHandler) loadStatistics(userID int64) statisticsView {
	return statisticsView{
		Version:   constant.Version,
		BuildTime: constant.BuildTime,
		History:   dal.History.CountByUserId(userID),
		Keywords:  dal.Keyword.GetByUserIdAndType(userID, model.PROJECT),
		Alarms:    dal.Keyword.GetByUserIdAndType(userID, model.ALARM),
	}
}

// StaticHandler answers /statistics with the counters and an inline keyboard.
func (c *CommandsHandler) StaticHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := update.Message.Chat.ID
	text, keyboard := renderStatisticsScreen(statisticsScreen{view: c.loadStatistics(chatID)})
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: keyboard,
	}); err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	}
}

// StatisticsCallback serves the inline keyboard of /statistics. The message is
// edited in place, so a click never adds a message to the chat.
func (c *CommandsHandler) StatisticsCallback(ctx context.Context, b *bot.Bot, update *models.Update) {
	query := update.CallbackQuery
	if query == nil {
		return
	}
	defer func() {
		if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
		}); err != nil {
			c.ctx.Logger.Error().Stack().Err(err).Msg("")
		}
	}()

	// The message can be inaccessible once it is too old or was deleted; there
	// is nothing to edit in that case.
	if query.Message.Message == nil {
		c.ctx.Logger.Debug().Msg("statistics callback refers to an inaccessible message")
		return
	}
	chatID := query.Message.Message.Chat.ID
	messageID := query.Message.Message.ID

	action, page, id, err := parseStatisticsCallback(query.Data)
	if err != nil {
		c.ctx.Logger.Warn().Err(err).Str("data", query.Data).Msg("ignoring statistics callback")
		return
	}

	view := c.loadStatistics(chatID)
	screen := statisticsScreen{view: view, kind: statisticsList, page: page - 1, id: id}

	switch action {
	case statisticsActionPage:
		// The screen is already a keyword list.
	case statisticsActionAlarms:
		screen.kind = statisticsAlarmList
		screen.page = 0
	case statisticsActionAlarmPg:
		screen.kind = statisticsAlarmList
	case statisticsActionBack:
		screen.page = 0
	case statisticsActionOpen:
		screen.kind = statisticsDetail
	case statisticsActionEdit:
		c.startKeywordEdit(ctx, b, view, chatID, messageID, id)
		return
	case statisticsActionDelete:
		screen.kind = statisticsDelete
	case statisticsActionDelOK:
		c.deleteKeyword(ctx, b, view, chatID, messageID, id)
		return
	case statisticsActionCancel:
		screen.kind = statisticsDetail
	default:
		c.ctx.Logger.Warn().Str("action", action).Msg("unknown statistics action")
		return
	}

	c.editStatisticsMessage(ctx, b, chatID, messageID, screen)
}

// startKeywordEdit arms a ForceReply prompt for the keyword and switches the
// report to the edit screen.
func (c *CommandsHandler) startKeywordEdit(ctx context.Context, b *bot.Bot, view statisticsView, chatID int64, messageID int, id int32) {
	keyword := statisticsFind(view, id)
	if keyword == nil {
		c.editStatisticsMessage(ctx, b, chatID, messageID, statisticsScreen{view: view})
		return
	}

	prompt, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      fmt.Sprintf("Send the new text for <code>#%d</code> %s, or /cancel.", id, statisticsEscaped(keyword.Keyword, statisticsTextLimit)),
		ParseMode: models.ParseModeHTML,
		ReplyMarkup: &models.ForceReply{
			ForceReply:            true,
			InputFieldPlaceholder: "new keyword",
		},
	})
	if err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
		return
	}

	c.pendingEdits.Store(chatID, pendingKeywordEdit{
		keywordID:       id,
		chatID:          chatID,
		messageID:       messageID,
		promptMessageID: prompt.ID,
		expires:         time.Now().Add(pendingKeywordEditTTL),
	})
	c.editStatisticsMessage(ctx, b, chatID, messageID, statisticsScreen{view: view, kind: statisticsEdit, id: id})
}

// deleteKeyword removes one keyword and returns to the list it came from.
func (c *CommandsHandler) deleteKeyword(ctx context.Context, b *bot.Bot, view statisticsView, chatID int64, messageID int, id int32) {
	keyword := statisticsFind(view, id)
	if keyword == nil {
		c.editStatisticsMessage(ctx, b, chatID, messageID, statisticsScreen{view: view})
		return
	}
	kind := statisticsList
	if model.KeywordType(keyword.Type) == model.ALARM {
		kind = statisticsAlarmList
	}
	if _, err := dal.Keyword.DeleteByIds(chatID, strconv.Itoa(int(id))); err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	}
	c.editStatisticsMessage(ctx, b, chatID, messageID, statisticsScreen{
		view: c.loadStatistics(chatID),
		kind: kind,
	})
}

// consumePendingKeywordEdit applies a reply to the ForceReply prompt of the edit
// screen. It reports whether the message was consumed: everything else falls
// through to the default handler.
func (c *CommandsHandler) consumePendingKeywordEdit(ctx context.Context, b *bot.Bot, update *models.Update) bool {
	message := update.Message
	if message == nil || strings.TrimSpace(message.Text) == "" {
		return false
	}
	chatID := message.Chat.ID

	value, ok := c.pendingEdits.Load(chatID)
	if !ok {
		return false
	}
	pending := value.(pendingKeywordEdit)
	if time.Now().After(pending.expires) {
		c.pendingEdits.Delete(chatID)
		return false
	}
	// A reply to a different message is not an answer to the prompt.
	if message.ReplyToMessage != nil && message.ReplyToMessage.ID != pending.promptMessageID {
		return false
	}

	text := strings.TrimSpace(message.Text)
	if strings.HasPrefix(text, "/") {
		// /cancel abandons the edit, any other command simply wins over it.
		c.pendingEdits.Delete(chatID)
		if strings.HasPrefix(text, "/cancel") {
			c.deletePromptMessage(ctx, b, chatID, pending.promptMessageID)
			c.refreshStatisticsDetail(ctx, b, pending)
		}
		return strings.HasPrefix(text, "/cancel")
	}

	c.pendingEdits.Delete(chatID)
	c.deletePromptMessage(ctx, b, chatID, pending.promptMessageID)
	if err := dal.Keyword.EditById(chatID, []string{fmt.Sprintf("%d=%s", pending.keywordID, text)}); err != nil {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
		if _, sendErr := b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Edit failed: " + err.Error(),
		}); sendErr != nil {
			c.ctx.Logger.Error().Stack().Err(sendErr).Msg("")
		}
	}
	c.refreshStatisticsDetail(ctx, b, pending)
	return true
}

// refreshStatisticsDetail re-renders the report as the detail screen of the
// keyword that was just edited.
func (c *CommandsHandler) refreshStatisticsDetail(ctx context.Context, b *bot.Bot, pending pendingKeywordEdit) {
	c.editStatisticsMessage(ctx, b, pending.chatID, pending.messageID, statisticsScreen{
		view: c.loadStatistics(pending.chatID),
		kind: statisticsDetail,
		id:   pending.keywordID,
	})
}

// deletePromptMessage removes the ForceReply prompt. A bot may delete its own
// message only while it is recent, so a failure is not worth reporting.
func (c *CommandsHandler) deletePromptMessage(ctx context.Context, b *bot.Bot, chatID int64, messageID int) {
	if messageID == 0 {
		return
	}
	if _, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{ChatID: chatID, MessageID: messageID}); err != nil {
		c.ctx.Logger.Debug().Stack().Err(err).Msg("delete statistics prompt")
	}
}

// editStatisticsMessage renders the screen and edits the report in place.
func (c *CommandsHandler) editStatisticsMessage(ctx context.Context, b *bot.Bot, chatID int64, messageID int, screen statisticsScreen) {
	text, keyboard := renderStatisticsScreen(screen)
	if _, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: keyboard,
	}); err != nil && !strings.Contains(err.Error(), "message is not modified") {
		c.ctx.Logger.Error().Stack().Err(err).Msg("")
	}
}

// renderStatisticsScreen renders the message text and the inline keyboard.
func renderStatisticsScreen(s statisticsScreen) (string, *models.InlineKeyboardMarkup) {
	switch s.kind {
	case statisticsAlarmList:
		return renderStatisticsAlarmList(s)
	case statisticsDetail:
		return renderStatisticsDetail(s)
	case statisticsEdit:
		return renderStatisticsEdit(s)
	case statisticsDelete:
		return renderStatisticsDelete(s)
	default:
		return renderStatisticsList(s)
	}
}

// renderStatisticsList renders the counters and one page of the keyword
// leaderboard: keywords are sorted by their number of related notices.
func renderStatisticsList(s statisticsScreen) (string, *models.InlineKeyboardMarkup) {
	keywords := statisticsSortedKeywords(s.view.Keywords)
	pages := statisticsPageCount(len(keywords))
	page := statisticsClampPage(s.page, pages)
	items := statisticsPageSlice(keywords, page)

	var b strings.Builder
	b.WriteString(statisticsHeadline(s.view))
	b.WriteString(statisticsOverview(s.view))
	fmt.Fprintf(&b, "\n<b>Related notices per keyword</b> (%s)\n", statisticsPageLabel(len(keywords), page, pages))
	if len(keywords) == 0 {
		b.WriteString("Use /add_keywords to subscribe to a keyword.\n")
	}
	for _, keyword := range items {
		fmt.Fprintf(&b, "<code>#%d</code> %s · <b>%s</b>\n",
			keywordID(keyword), statisticsEscaped(keyword.Keyword, statisticsTextLimit), groupDigits(int64(keyword.Counter)))
	}

	rows := statisticsKeywordRows(items)
	rows = append(rows, statisticsPagerRows(page, pages, statisticsActionPage)...)
	if len(s.view.Alarms) > 0 {
		rows = append(rows, []models.InlineKeyboardButton{{
			Text:         fmt.Sprintf("Alarm keywords (%d)", len(s.view.Alarms)),
			CallbackData: statisticsCallbackData(statisticsActionAlarms, 1, 0),
		}})
	}
	return strings.TrimSpace(b.String()), statisticsKeyboard(rows)
}

// renderStatisticsAlarmList renders one page of the alarm keywords. They carry
// no notice counter, so the entry is the id and the text only.
func renderStatisticsAlarmList(s statisticsScreen) (string, *models.InlineKeyboardMarkup) {
	alarms := statisticsSortedAlarms(s.view.Alarms)
	pages := statisticsPageCount(len(alarms))
	page := statisticsClampPage(s.page, pages)
	items := statisticsPageSlice(alarms, page)

	var b strings.Builder
	b.WriteString(statisticsHeadline(s.view))
	b.WriteString(statisticsOverview(s.view))
	fmt.Fprintf(&b, "\n<b>Alarm keywords</b> (%s)\n", statisticsPageLabel(len(alarms), page, pages))
	if len(alarms) == 0 {
		b.WriteString("Use /add_alarm_keywords to subscribe to an alarm keyword.\n")
	}
	for _, keyword := range items {
		fmt.Fprintf(&b, "<code>#%d</code> %s\n", keywordID(keyword), statisticsEscaped(keyword.Keyword, statisticsTextLimit))
	}

	rows := statisticsKeywordRows(items)
	rows = append(rows, statisticsPagerRows(page, pages, statisticsActionAlarmPg)...)
	rows = append(rows, []models.InlineKeyboardButton{{
		Text:         "‹ Back to statistics",
		CallbackData: statisticsCallbackData(statisticsActionBack, 1, 0),
	}})
	return strings.TrimSpace(b.String()), statisticsKeyboard(rows)
}

// renderStatisticsDetail renders one keyword: its type, counter and creation
// date, plus the actions that are available for it.
func renderStatisticsDetail(s statisticsScreen) (string, *models.InlineKeyboardMarkup) {
	keyword := statisticsFind(s.view, s.id)
	if keyword == nil {
		return renderStatisticsList(statisticsScreen{view: s.view})
	}
	alarm := model.KeywordType(keyword.Type) == model.ALARM

	var b strings.Builder
	title := "Keyword"
	if alarm {
		title = "Alarm keyword"
	}
	fmt.Fprintf(&b, "<b>%s</b> <code>#%d</code>\n", title, keywordID(keyword))
	fmt.Fprintf(&b, "<b>%s</b>\n\n", statisticsEscaped(keyword.Keyword, statisticsTextLimit))
	fmt.Fprintf(&b, "Type: <b>%s</b>\n", statisticsTypeName(keyword.Type))
	if !alarm {
		fmt.Fprintf(&b, "Related notices: <b>%s</b>\n", groupDigits(int64(keyword.Counter)))
	}
	fmt.Fprintf(&b, "Created: <b>%s</b>\n", keyword.CreatedAt.Format("2006-01-02"))

	back := statisticsActionBack
	if alarm {
		back = statisticsActionAlarms
	}
	return strings.TrimSpace(b.String()), statisticsKeyboard([][]models.InlineKeyboardButton{
		{
			{Text: "✏️ Edit", CallbackData: statisticsCallbackData(statisticsActionEdit, 1, keywordID(keyword))},
			{Text: "🗑 Delete", CallbackData: statisticsCallbackData(statisticsActionDelete, 1, keywordID(keyword))},
		},
		{{Text: "‹ Back", CallbackData: statisticsCallbackData(back, 1, 0)}},
	})
}

// renderStatisticsEdit renders the prompt shown while the ForceReply is armed.
func renderStatisticsEdit(s statisticsScreen) (string, *models.InlineKeyboardMarkup) {
	keyword := statisticsFind(s.view, s.id)
	if keyword == nil {
		return renderStatisticsList(statisticsScreen{view: s.view})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>Edit keyword</b> <code>#%d</code>\n", keywordID(keyword))
	fmt.Fprintf(&b, "Send the new text for <b>%s</b>.\n", statisticsEscaped(keyword.Keyword, statisticsTextLimit))
	return strings.TrimSpace(b.String()), statisticsKeyboard([][]models.InlineKeyboardButton{
		{{Text: "Cancel", CallbackData: statisticsCallbackData(statisticsActionCancel, 1, keywordID(keyword))}},
	})
}

// renderStatisticsDelete renders the delete confirmation.
func renderStatisticsDelete(s statisticsScreen) (string, *models.InlineKeyboardMarkup) {
	keyword := statisticsFind(s.view, s.id)
	if keyword == nil {
		return renderStatisticsList(statisticsScreen{view: s.view})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>Delete keyword</b> <code>#%d</code>\n", keywordID(keyword))
	if model.KeywordType(keyword.Type) == model.ALARM {
		fmt.Fprintf(&b, "Remove <b>%s</b>? Alarm records already delivered stay in history.\n",
			statisticsEscaped(keyword.Keyword, statisticsTextLimit))
	} else {
		fmt.Fprintf(&b, "Remove <b>%s</b>? Its <b>%s</b> related notices stay in history, but they stop counting for this keyword.\n",
			statisticsEscaped(keyword.Keyword, statisticsTextLimit), groupDigits(int64(keyword.Counter)))
	}
	return strings.TrimSpace(b.String()), statisticsKeyboard([][]models.InlineKeyboardButton{
		{{Text: fmt.Sprintf("Delete #%d", keywordID(keyword)), CallbackData: statisticsCallbackData(statisticsActionDelOK, 1, keywordID(keyword))}},
		{{Text: "Cancel", CallbackData: statisticsCallbackData(statisticsActionCancel, 1, keywordID(keyword))}},
	})
}

// statisticsHeadline renders the bot version and build time.
func statisticsHeadline(v statisticsView) string {
	return fmt.Sprintf("<b>magnet-bot</b> <code>%s</code>\nBuild <code>%s</code>\n",
		html.EscapeString(v.Version), html.EscapeString(v.BuildTime))
}

// statisticsOverview renders the four counters of the report.
func statisticsOverview(v statisticsView) string {
	var related int64
	for _, keyword := range v.Keywords {
		related += int64(keyword.Counter)
	}

	var b strings.Builder
	b.WriteString("\n<b>Overview</b>\n")
	fmt.Fprintf(&b, "History: <b>%s</b>\n", groupDigits(v.History))
	fmt.Fprintf(&b, "Keywords: <b>%d</b>\n", len(v.Keywords))
	fmt.Fprintf(&b, "Related notices: <b>%s</b>\n", groupDigits(related))
	fmt.Fprintf(&b, "Alarm keywords: <b>%d</b>\n", len(v.Alarms))
	return b.String()
}

// statisticsKeyboard wraps the rows of a keyboard. It returns nil when there is
// nothing to press: Telegram rejects an empty inline keyboard.
func statisticsKeyboard(rows [][]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	if len(rows) == 0 {
		return nil
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// statisticsKeywordRows lays the buttons of one page out two per row.
func statisticsKeywordRows(items []*model.Keyword) [][]models.InlineKeyboardButton {
	rows := make([][]models.InlineKeyboardButton, 0, (len(items)+1)/2)
	for i := 0; i < len(items); i += 2 {
		row := make([]models.InlineKeyboardButton, 0, 2)
		for _, keyword := range items[i:min(i+2, len(items))] {
			row = append(row, models.InlineKeyboardButton{
				Text:         fmt.Sprintf("#%d %s", keywordID(keyword), statisticsLabel(keyword.Keyword, statisticsButtonLimit)),
				CallbackData: statisticsCallbackData(statisticsActionOpen, 1, keywordID(keyword)),
			})
		}
		rows = append(rows, row)
	}
	return rows
}

// statisticsPagerRows renders the previous/next row of a list. It is omitted for
// a single page, and an arrow is only drawn when there is somewhere to go.
func statisticsPagerRows(page, pages int, action string) [][]models.InlineKeyboardButton {
	if pages <= 1 {
		return nil
	}
	row := make([]models.InlineKeyboardButton, 0, 3)
	if page > 0 {
		row = append(row, models.InlineKeyboardButton{
			Text:         "‹",
			CallbackData: statisticsCallbackData(action, page, 0),
		})
	}
	row = append(row, models.InlineKeyboardButton{
		Text:         fmt.Sprintf("%d/%d", page+1, pages),
		CallbackData: statisticsCallbackData(action, page+1, 0),
	})
	if page < pages-1 {
		row = append(row, models.InlineKeyboardButton{
			Text:         "›",
			CallbackData: statisticsCallbackData(action, page+2, 0),
		})
	}
	return [][]models.InlineKeyboardButton{row}
}

// statisticsCallbackData builds one button's "stats:<action>:<page>:<id>"
// payload. Telegram caps callback data at 64 bytes; the payload stays far below
// that.
func statisticsCallbackData(action string, page int, id int32) string {
	return fmt.Sprintf("%s%s:%d:%d", constant.StatisticsCallback, action, page, id)
}

// parseStatisticsCallback splits the callback payload into its parts. Telegram
// relays whatever a client sends, so every part is validated here instead of
// being indexed blindly.
func parseStatisticsCallback(data string) (action string, page int, id int32, err error) {
	rest, ok := strings.CutPrefix(data, constant.StatisticsCallback)
	if !ok {
		return "", 0, 0, errMalformedStatisticsCallback
	}
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		return "", 0, 0, errMalformedStatisticsCallback
	}
	action = parts[0]
	if action == "" {
		return "", 0, 0, errMalformedStatisticsCallback
	}
	page, err = strconv.Atoi(parts[1])
	if err != nil || page < 1 {
		return "", 0, 0, fmt.Errorf("invalid statistics page %q", parts[1])
	}
	raw, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil || raw < 0 {
		return "", 0, 0, fmt.Errorf("invalid statistics id %q", parts[2])
	}
	return action, page, int32(raw), nil
}

// statisticsFind looks a keyword up in the loaded view, tolerating an id that
// disappeared while the message was open.
func statisticsFind(v statisticsView, id int32) *model.Keyword {
	for _, keyword := range v.Keywords {
		if keywordID(keyword) == id {
			return keyword
		}
	}
	for _, keyword := range v.Alarms {
		if keywordID(keyword) == id {
			return keyword
		}
	}
	return nil
}

// statisticsSortedKeywords is the leaderboard order: most related notices first,
// then the id, so equal counters keep a stable order.
func statisticsSortedKeywords(list []*model.Keyword) []*model.Keyword {
	sorted := slices.Clone(list)
	slices.SortStableFunc(sorted, func(a, c *model.Keyword) int {
		if a.Counter != c.Counter {
			return cmp.Compare(c.Counter, a.Counter)
		}
		return cmp.Compare(keywordID(a), keywordID(c))
	})
	return sorted
}

// statisticsSortedAlarms lists the alarm keywords in creation order.
func statisticsSortedAlarms(list []*model.Keyword) []*model.Keyword {
	sorted := slices.Clone(list)
	slices.SortStableFunc(sorted, func(a, c *model.Keyword) int {
		return cmp.Compare(keywordID(a), keywordID(c))
	})
	return sorted
}

// statisticsPageCount returns how many pages hold total entries.
func statisticsPageCount(total int) int {
	return max(1, (total+statisticsPageSize-1)/statisticsPageSize)
}

// statisticsClampPage keeps a page inside the list, so a stale callback (or a
// delete that emptied the last page) still renders something.
func statisticsClampPage(page, pages int) int {
	return min(max(page, 0), pages-1)
}

// statisticsPageSlice returns the entries of one page.
func statisticsPageSlice(list []*model.Keyword, page int) []*model.Keyword {
	start := page * statisticsPageSize
	if start >= len(list) {
		return nil
	}
	return list[start:min(start+statisticsPageSize, len(list))]
}

// statisticsPageLabel renders "39 · 2/5", or just the total for a single page.
func statisticsPageLabel(total, page, pages int) string {
	if pages <= 1 {
		return strconv.Itoa(total)
	}
	return fmt.Sprintf("%d · %d/%d", total, page+1, pages)
}

// statisticsTypeName renders the keyword type shown on the detail screen.
func statisticsTypeName(t int32) string {
	if model.KeywordType(t) == model.ALARM {
		return "alarm"
	}
	return "project"
}

// statisticsLabel flattens user text onto one line and shortens a pathological
// value. A pasted newline would otherwise break the one-entry-per-line layout,
// and a pasted paragraph would push the message past Telegram's size limit.
func statisticsLabel(text string, limit int) string {
	flat := strings.Join(strings.Fields(text), " ")
	runes := []rune(flat)
	if len(runes) <= limit {
		return flat
	}
	return string(runes[:limit-1]) + "…"
}

// statisticsEscaped is statisticsLabel followed by HTML escaping: keyword text
// is user input rendered inside an HTML message.
func statisticsEscaped(text string, limit int) string {
	return html.EscapeString(statisticsLabel(text, limit))
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
