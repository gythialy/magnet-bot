package handler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/server"
	"github.com/gythialy/magnet/pkg/utils"
)

const (
	// accessLastSeenInterval is how often an authorised account's last_seen is
	// written. A write per update would turn every message into a database
	// write; one a minute is enough for an operator to see who is active.
	accessLastSeenInterval = time.Minute

	// accessNoticeInterval bounds the "access denied" reply per account. A
	// stranger retrying in a loop must not be able to make the bot answer every
	// attempt.
	accessNoticeInterval = 10 * time.Minute

	// accessDeniedLimit is the size of the in-memory denial history exposed to
	// the administrator status view.
	accessDeniedLimit = 50

	// accessWebhookSnapshotTimeout bounds the getWebhookInfo call made while
	// rendering the administrator status.
	accessWebhookSnapshotTimeout = 5 * time.Second
)

// DeniedEntry is one rejected access attempt, kept in memory for the
// administrator status view. It deliberately carries no message text: the bot
// records who tried, not what they wrote.
type DeniedEntry struct {
	UserID   int64
	Username string
	Name     string
	At       time.Time
}

// Messages of the access-control rejection path.
const (
	accessDeniedCallbackText = "Access denied. This bot is private."
	accessDeniedMessageText  = "Access denied. This bot is private. Ask the owner for access."
)

// accessGuard holds the process-local access-control state: the throttles and
// the denial history. Its zero value is ready to use, so a partial BotContext
// built in a test works without an initialiser.
type accessGuard struct {
	mu       sync.Mutex
	lastSeen map[int64]time.Time
	notices  map[int64]time.Time
	denied   []DeniedEntry
}

// allowLastSeen reports whether last_seen should be written for userID now. It
// also drops expired throttle entries, which keeps the map bounded by the
// accounts active in the current window instead of growing with history.
func (g *accessGuard) allowLastSeen(userID int64, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.lastSeen == nil {
		g.lastSeen = make(map[int64]time.Time)
	}
	for id, at := range g.lastSeen {
		if now.Sub(at) >= accessLastSeenInterval {
			delete(g.lastSeen, id)
		}
	}
	if at, ok := g.lastSeen[userID]; ok && now.Sub(at) < accessLastSeenInterval {
		return false
	}
	g.lastSeen[userID] = now
	return true
}

// allowNotice reports whether the "access denied" reply should be sent to
// userID now, and records the attempt when it should.
func (g *accessGuard) allowNotice(userID int64, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.notices == nil {
		g.notices = make(map[int64]time.Time)
	}
	for id, at := range g.notices {
		if now.Sub(at) >= accessNoticeInterval {
			delete(g.notices, id)
		}
	}
	if at, ok := g.notices[userID]; ok && now.Sub(at) < accessNoticeInterval {
		return false
	}
	g.notices[userID] = now
	return true
}

// recordDenied appends one rejection to the bounded history. The oldest entry
// is dropped on overflow; the copy keeps the dropped entries collectable.
func (g *accessGuard) recordDenied(entry DeniedEntry) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.denied = append(g.denied, entry)
	if len(g.denied) > accessDeniedLimit {
		g.denied = append([]DeniedEntry(nil), g.denied[len(g.denied)-accessDeniedLimit:]...)
	}
}

// recentDenied returns a snapshot of the denial history, newest last.
func (g *accessGuard) recentDenied() []DeniedEntry {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]DeniedEntry(nil), g.denied...)
}

// accessActor is the part of an update the access-control layer needs.
type accessActor struct {
	id       int64
	username string
	name     string
}

// accessMiddleware enforces the bot's access policy for every update:
//
//  1. an update without an identifiable account is let through, because the
//     bot's handlers ignore it anyway (channel posts, service messages);
//  2. anything that is not a private chat is dropped without a reply — the bot
//     intentionally does not answer in groups or channels;
//  3. an allowed account gets its last_seen refreshed (throttled) and reaches
//     the handler;
//  4. everybody else is rejected: callbacks get an answer so the client stops
//     spinning, messages get a single reply per window, and the attempt is
//     recorded in memory.
func (ctx *BotContext) accessMiddleware(next bot.HandlerFunc) bot.HandlerFunc {
	return func(hctx context.Context, b *bot.Bot, update *models.Update) {
		actor, chatType, knownChat := accessUpdateIdentity(update)

		if actor.id <= 0 {
			next(hctx, b, update)
			return
		}
		// An update whose chat type cannot be determined is denied: the
		// private-chat-only rule must fail closed rather than guess.
		if !knownChat || chatType != models.ChatTypePrivate {
			return
		}

		if dal.User.IsAllowed(actor.id) {
			ctx.touchLastSeen(actor.id)
			next(hctx, b, update)
			return
		}

		ctx.rejectAccess(hctx, b, update, actor)
	}
}

// touchLastSeen records activity at most once per accessLastSeenInterval.
func (ctx *BotContext) touchLastSeen(userID int64) {
	now := time.Now()
	if !ctx.access.allowLastSeen(userID, now) {
		return
	}

	// A missing row is normal in "off" mode, where every account is allowed
	// without one; only a real storage failure is worth a log line.
	if err := dal.User.SetLastSeen(userID, now); err != nil && !errors.Is(err, dal.ErrNotFound) {
		ctx.Logger.Warn().Err(err).Msg("record user last seen")
	}
}

// rejectAccess answers and records one denied update.
func (ctx *BotContext) rejectAccess(hctx context.Context, b *bot.Bot, update *models.Update, actor accessActor) {
	now := time.Now()
	ctx.access.recordDenied(DeniedEntry{
		UserID:   actor.id,
		Username: actor.username,
		Name:     actor.name,
		At:       now,
	})

	if update.CallbackQuery != nil {
		// Telegram shows nothing to the user without an answer, and the client
		// keeps the spinner until one arrives.
		if _, err := b.AnswerCallbackQuery(hctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            accessDeniedCallbackText,
		}); err != nil {
			ctx.Logger.Warn().Err(err).Msg("answer denied callback query")
		}
		return
	}

	message := accessUpdateMessage(update)
	if message == nil {
		return
	}
	if !ctx.access.allowNotice(actor.id, now) {
		return
	}
	if _, err := b.SendMessage(hctx, &bot.SendMessageParams{
		ChatID: message.Chat.ID,
		Text:   accessDeniedMessageText,
	}); err != nil {
		ctx.Logger.Warn().Err(err).Msg("send access denial")
	}
}

// IsAdmin reports whether userID is an enabled administrator. Rights are
// independent of the access mode: an open bot does not promote every account.
func (ctx *BotContext) IsAdmin(userID int64) bool {
	return dal.User.IsAdmin(userID)
}

// RecentDenied returns the most recent denied access attempts, oldest first.
// The slice is a copy: callers may keep it while new attempts are recorded.
func (ctx *BotContext) RecentDenied() []DeniedEntry {
	return ctx.access.recentDenied()
}

// RecentDeniedEntries is the accessor the administrator status provider uses.
// It is an alias of RecentDenied kept under the name the P5 API is expected to
// call, so the server package does not need a second implementation.
func (ctx *BotContext) RecentDeniedEntries() []DeniedEntry {
	return ctx.RecentDenied()
}

// BotContext is the AdminStatusProvider the Mini App API receives through
// APIConfig.Status. The assertion makes a future interface change a compile
// error here instead of at the composition root.
var _ server.AdminStatusProvider = (*BotContext)(nil)

// SystemStatus implements server.AdminStatusProvider: it projects the local
// denial history and the Telegram webhook registration into the shape the
// administrator status screen renders.
//
// The webhook is only inspected in webhook mode. In polling mode there is no
// registration to report, and asking Telegram anyway could surface a stale one
// left by an earlier deployment.
func (ctx *BotContext) SystemStatus(statusCtx context.Context) server.AdminStatus {
	status := server.AdminStatus{}

	if denied := ctx.RecentDenied(); len(denied) > 0 {
		status.RecentDenied = make([]server.DeniedUser, 0, len(denied))
		for _, entry := range denied {
			status.RecentDenied = append(status.RecentDenied, server.DeniedUser{
				UserID:   entry.UserID,
				Username: entry.Username,
				Name:     entry.Name,
				At:       entry.At,
			})
		}
	}

	if ctx.Config != nil && ctx.Config.PublicEnabled() && ctx.Config.TelegramWebhookEnabled {
		if url, lastError, pending, ok := ctx.WebhookSnapshot(statusCtx); ok {
			status.Webhook = &server.WebhookStatus{
				URL:       url,
				LastError: lastError,
				Pending:   pending,
			}
		}
	}

	return status
}

// WebhookSnapshot returns the current Telegram webhook registration. It is a
// best-effort view for the administrator status page: the call is bounded and a
// failure is reported as ok=false instead of an error the status page cannot
// render.
func (ctx *BotContext) WebhookSnapshot(parent context.Context) (url, lastError string, pending int, ok bool) {
	if ctx.Bot == nil {
		return "", "", 0, false
	}
	if parent == nil {
		parent = context.Background()
	}

	snapshotCtx, cancel := context.WithTimeout(parent, accessWebhookSnapshotTimeout)
	defer cancel()

	info, err := ctx.Bot.GetWebhookInfo(snapshotCtx)
	if err != nil || info == nil {
		return "", "", 0, false
	}
	return info.URL, info.LastErrorMessage, info.PendingUpdateCount, true
}

// logAccessBootstrap records the access mode chosen (or kept) at startup.
// Administrator ids are only listed at debug: the info line is what operators
// need, and a shared log usually should not collect account identifiers.
func logAccessBootstrap(logger *utils.Logger, mode string, managerIDs []int64) {
	switch mode {
	case model.AccessModeRestricted:
		logger.Info().
			Int64("administrators", dal.User.CountActiveAdmins()).
			Msg("access control is restricted")
		if len(managerIDs) > 0 {
			logger.Debug().Interface("manager_ids", managerIDs).
				Msg("access control administrators seeded from the environment")
		}
	case model.AccessModeOff:
		logger.Warn().Msg("access control is off: no administrator configured")
	default:
		logger.Info().Str("mode", mode).Msg("access control mode")
	}
}

// accessUpdateMessage returns the message carried by an update, if any.
func accessUpdateMessage(update *models.Update) *models.Message {
	if update == nil {
		return nil
	}
	if update.Message != nil {
		return update.Message
	}
	return update.EditedMessage
}

// accessUpdateIdentity extracts the account and the chat type of one update.
// knownChat is false when the update does not carry a chat the middleware can
// inspect, which is treated as "not a private chat".
func accessUpdateIdentity(update *models.Update) (actor accessActor, chatType models.ChatType, knownChat bool) {
	if update == nil {
		return accessActor{}, "", false
	}

	if message := accessUpdateMessage(update); message != nil {
		return actorFromUser(message.From), message.Chat.Type, true
	}

	if query := update.CallbackQuery; query != nil {
		actor = actorFromUser(&query.From)
		switch {
		case query.Message.Message != nil:
			return actor, query.Message.Message.Chat.Type, true
		case query.Message.InaccessibleMessage != nil:
			return actor, query.Message.InaccessibleMessage.Chat.Type, true
		default:
			return actor, "", false
		}
	}

	return accessActor{}, "", false
}

// actorFromUser normalises the sender of an update. A missing sender yields the
// zero actor, which the middleware treats as "no access decision".
func actorFromUser(user *models.User) accessActor {
	if user == nil || user.ID <= 0 {
		return accessActor{}
	}
	return accessActor{
		id:       user.ID,
		username: user.Username,
		name:     userDisplayName(user),
	}
}

// userDisplayName builds the human label kept in the denial history.
func userDisplayName(user *models.User) string {
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if name == "" {
		return user.Username
	}
	return name
}
