package handler

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"time"

	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/constant"
	"github.com/gythialy/magnet/pkg/dal"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/server"
	"github.com/gythialy/magnet/pkg/utils"

	"github.com/glebarez/sqlite"
	"github.com/go-co-op/gocron"
	"github.com/go-telegram/bot"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type dbLogger struct {
	*utils.Logger
}

func (dl *dbLogger) LogMode(_ logger.LogLevel) logger.Interface {
	return dl
}

func (dl *dbLogger) Info(_ context.Context, msg string, data ...any) {
	dl.Logger.Info().Msgf(msg, data...)
}

func (dl *dbLogger) Warn(_ context.Context, msg string, data ...any) {
	dl.Logger.Warn().Msgf(msg, data...)
}

func (dl *dbLogger) Error(_ context.Context, msg string, data ...any) {
	dl.Logger.Error().Msgf(msg, data...)
}

func (dl *dbLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)
	sql, rows := fc()
	if err != nil {
		dl.Logger.Error().Msgf("[%.3fms] [rows:%v] %s; %s", float64(elapsed.Nanoseconds())/1e6, rows, sql, err)
	} else {
		dl.Logger.Info().Msgf("[%.3fms] [rows:%v] %s", float64(elapsed.Nanoseconds())/1e6, rows, sql)
	}
}

type BotContext struct {
	Bot        *bot.Bot
	Store      *Store[model.RequestInfo]
	Logger     *utils.Logger
	Config     *config.ServiceConfig
	Gotenberg  *GotenbergClient
	ctx        context.Context
	cancel     context.CancelFunc
	scheduler  *gocron.Scheduler
	processor  *InfoProcessor
	retrier    *AttachmentRetrier
	cmdHandler *CommandsHandler
	httpServer *server.Server
	access     accessGuard
}

// context returns the process-wide context so background work honours
// shutdown. It never returns nil, which keeps it usable from tests that build
// a partial BotContext.
func (ctx *BotContext) context() context.Context {
	if ctx.ctx != nil {
		return ctx.ctx
	}
	return context.Background()
}

func NewBotContext() (*BotContext, error) {
	cfg := config.NewServiceConfig()
	// Validate before anything is built: a malformed PUBLIC_BASE_URL, or a
	// webhook enabled without its secret, must stop the process at startup
	// instead of turning every Telegram delivery into a 403.
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	ctxLogger := utils.Configure(utils.Config{
		ConsoleLoggingEnabled: true,
		EncodeLogsAsJson:      false,
		FileLoggingEnabled:    true,
		Directory:             cfg.BaseDir,
		Filename:              constant.LogFile,
		MaxSize:               10,
		MaxBackups:            10,
		MaxAge:                7,
		LogLevel:              cfg.LogLevel,
	})

	// Not fatal: the Mini App is served from the same listener, so it stays
	// usable against a local reverse proxy while debugging.
	if cfg.MiniAppEnabled && !cfg.PublicEnabled() {
		ctxLogger.Warn().
			Msg("MINI_APP_ENABLED is true but PUBLIC_BASE_URL is empty: the Mini App is only reachable locally")
	}

	db, err := gorm.Open(sqlite.Open(path.Join(cfg.BaseDir, constant.DatabaseFile)), &gorm.Config{
		Logger: &dbLogger{ctxLogger},
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(
		&model.Keyword{},
		&model.History{},
		&model.Alarm{},
		&model.KeywordNotice{},
		&model.PendingAttachment{},
		&model.User{},
		&model.Setting{},
	); err != nil {
		return nil, err
	}
	dal.SetDefault(db)

	// The access tables are the first thing every update reads, so they must be
	// migrated and seeded before the bot serves anything. A bootstrap failure
	// is fatal on purpose: starting with a half-written authorisation table
	// would either lock every account out or let everybody in.
	accessMode, err := dal.EnsureBootstrap(cfg.ManagerIDs, cfg.AllowedUserIDs)
	if err != nil {
		return nil, fmt.Errorf("bootstrap access control: %w", err)
	}
	logAccessBootstrap(ctxLogger, accessMode, cfg.ManagerIDs)

	// Backfill: history rows written before links were absolute carry a
	// scheme-less URL, which would no longer match the claim key of new pushes
	// and make already delivered notices look new.
	if rows, err := dal.History.NormalizeURLs(); err != nil {
		ctxLogger.Error().Stack().Err(err).Msg("normalize history urls")
	} else if rows > 0 {
		ctxLogger.Info().Int64("rows", rows).Msg("normalized scheme-less history urls")
	}

	// The keyword-match log only has to cover the crawl window.
	if rows, err := dal.PruneKeywordMatches(time.Now()); err != nil {
		ctxLogger.Error().Stack().Err(err).Msg("prune keyword matches")
	} else if rows > 0 {
		ctxLogger.Debug().Int64("rows", rows).Msg("pruned keyword match records")
	}

	client, err := NewGotenbergClient(cfg.PDF.PDFServiceURL, cfg.PDF.WebhookURL(), cfg.PDF.WebhookToken)
	if err != nil {
		return nil, fmt.Errorf("create gotenberg client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	botContext := &BotContext{
		ctx:       ctx,
		cancel:    cancel,
		scheduler: gocron.NewScheduler(schedulerLocation()),
		Logger:    ctxLogger,
		Config:    cfg,
		Store:     NewStore[model.RequestInfo](),
		Gotenberg: client,
	}

	botContext.cmdHandler = NewCommandsHandler(botContext)

	botOptions := []bot.Option{
		// The access middleware wraps every handler, including the default
		// handler, so an unauthorised account is rejected before any command
		// or fallback logic sees the update.
		bot.WithMiddlewares(botContext.accessMiddleware),
		bot.WithDefaultHandler((&defaultHandler{cmd: botContext.cmdHandler}).Handler),
		bot.WithHTTPClient(time.Minute, &http.Client{Timeout: 2 * time.Minute}),
	}
	// Only set the secret when one is configured. The library would treat an
	// empty value as "no check" anyway, but leaving the option out keeps the
	// intent visible instead of relying on that coincidence.
	if cfg.WebhookSecret != "" {
		botOptions = append(botOptions, bot.WithWebhookSecretToken(cfg.WebhookSecret))
	}

	telegramBot, err := bot.New(config.TelegramToken(), botOptions...)
	if err != nil {
		return nil, err
	}
	botContext.Bot = telegramBot

	if err := botContext.initBot(); err != nil {
		return nil, err
	}

	processor, err := NewInfoProcessor(botContext)
	if err != nil {
		return nil, err
	}
	botContext.processor = processor
	botContext.retrier = NewAttachmentRetrier(botContext)
	return botContext, nil
}

// schedulerLocation returns the Asia/Shanghai location, falling back to a
// fixed +08:00 zone on hosts without tzdata (the distroless image ships none).
func schedulerLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*60*60)
}

func (ctx *BotContext) initBot() error {
	cmdHandler := ctx.cmdHandler
	miniApp := ctx.miniAppEnabled()

	// One registration decision for every command: while the Mini App is
	// available, a retired command is answered with its prompt instead of the
	// live handler. Registration and the fallback dispatch share the same
	// retired set, so the two cannot drift apart.
	register := func(command string, handler bot.HandlerFunc) {
		ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, command, bot.MatchTypePrefix,
			ctx.commandHandler(command, handler))
	}

	register(constant.Me, MeHandler)
	register(constant.Debug, DebugHandler)
	register(constant.Start, (&startHandler{cmdHandler: cmdHandler}).Handler)
	register(constant.AddKeyword, cmdHandler.AddKeywordHandler)
	register(constant.DeleteKeyword, cmdHandler.DeleteKeywordHandler)
	register(constant.EditKeyword, cmdHandler.EditKeywordHandler)
	register(constant.AddAlarmKeyword, cmdHandler.AddAlarmKeywordHandler)
	register(constant.SearchAlarmRecords, cmdHandler.SearchAlarmRecordHandler)
	register(constant.Alarm, cmdHandler.AlarmRecordHandler)
	register(constant.SearchHistory, cmdHandler.SearchHistoryHandler)
	register(constant.ListToday, cmdHandler.ListTodayHandler)
	register(constant.ConvertPDF, cmdHandler.ConvertURLToPDFHandler)
	register(constant.ConvertIMG, cmdHandler.ConvertURLToIMGHandler)
	register(constant.Statistics, cmdHandler.StaticHandler)

	// /settings is administrator-only but deliberately not retired: it must
	// keep working in both modes, and the handler enforces the role itself. It
	// therefore bypasses the retirement switch above and is registered directly.
	ctx.Bot.RegisterHandler(bot.HandlerTypeMessageText, SettingsCommand, bot.MatchTypePrefix, ctx.adminSettingsHandler)

	// The callback handlers stay registered regardless of the Mini App: they
	// serve messages users already have in their chats, and a stale "stats:",
	// "search:" or "today:" button must not start failing.
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.SearchCallback, bot.MatchTypePrefix, cmdHandler.HandleCallbackQuery)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.AlarmCallback, bot.MatchTypePrefix, cmdHandler.HandleCallbackQuery)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.TodayCallback, bot.MatchTypePrefix, cmdHandler.HandleCallbackQuery)
	ctx.Bot.RegisterHandler(bot.HandlerTypeCallbackQueryData, constant.StatisticsCallback, bot.MatchTypePrefix, cmdHandler.StatisticsCallback)

	managerHandler := NewManagerHandler(ctx)
	register(constant.Retry, managerHandler.Retry)
	register(constant.Clean, managerHandler.Clean)

	_, err := ctx.Bot.SetMyCommands(ctx.context(), &bot.SetMyCommandsParams{
		Commands: TelegramCommands(miniApp),
	})
	if err != nil {
		return err
	}

	// The menu button is an affordance on top of the command list: a failed
	// update must not keep the bot from starting, so it is only logged.
	if button := ctx.chatMenuButton(); button != nil {
		if _, err := ctx.Bot.SetChatMenuButton(ctx.context(), &bot.SetChatMenuButtonParams{
			MenuButton: *button,
		}); err != nil {
			ctx.Logger.Warn().Err(err).Msg("telegram setChatMenuButton failed")
		}
	}
	return nil
}

const (
	// webhookRetryInterval is how long a failed setWebhook waits before the
	// registration is attempted again.
	webhookRetryInterval = 30 * time.Second
	// webhookSetupTimeout bounds the synchronous deleteWebhook call so an
	// unreachable API cannot stall startup for the HTTP client's full timeout.
	webhookSetupTimeout = 10 * time.Second
	// serverShutdownTimeout is the grace period given to in-flight requests.
	serverShutdownTimeout = 5 * time.Second
	// miniAppInitDataTTL bounds how long a signed initData is accepted. An hour
	// matches the plan: long enough for a browsing session, short enough that a
	// leaked payload stops working the same day.
	miniAppInitDataTTL = time.Hour
)

// Start binds the HTTP listener, starts Telegram delivery in the mode the
// configuration selects, and launches the background workers. The only error
// returned is a listener that could not bind: everything after that is retried
// or logged, because a bot whose Telegram API is temporarily unreachable still
// serves the Gotenberg callback and the scheduler.
func (ctx *BotContext) Start() error {
	if ctx.Config.PDF.WebhookServerPort == 0 && !ctx.Config.PublicEnabled() {
		// Port 0 picks an ephemeral port nobody knows, so Gotenberg's callback
		// to {WEBHOOK_SERVER_URL}:0 can never arrive.
		ctx.Logger.Warn().
			Msg("WEBHOOK_SERVER_PORT is 0 and PUBLIC_BASE_URL is empty: the Gotenberg callback address is unreachable")
	}

	// Telegram accepts updates through getUpdates or the webhook, never both,
	// so the push endpoint is mounted only in webhook mode. In polling mode a
	// leftover `/telegram/webhook` would be an unauthenticated way to inject
	// updates into the process; leaving it unregistered answers 404 instead.
	webhookMode := ctx.Config.PublicEnabled() && ctx.Config.TelegramWebhookEnabled
	var telegramUpdate http.HandlerFunc
	if webhookMode {
		telegramUpdate = ctx.telegramWebhook(ctx.Bot.WebhookHandler())
	}

	// The Mini App is optional. When it is disabled neither its assets nor its
	// API are registered, so a chat-only deployment exposes no extra surface on
	// the internal listener.
	var staticHandler, apiHandler http.Handler
	if ctx.Config.MiniAppEnabled {
		staticHandler = server.NewStaticHandler()
		apiHandler = server.NewAPI(server.APIConfig{
			BotToken:    config.TelegramToken(),
			BotUsername: config.TelegramName(),
			InitDataTTL: miniAppInitDataTTL,
			// The bot context itself starts conversions, so the API and the
			// chat command share one code path and one placeholder message
			// convention instead of two that can drift.
			Conversion:  ctx,
			AllowedHost: ctx.Config.MessageServerUrl,
			// The admin status page reads the same in-memory denial log and the
			// Telegram webhook state this context already owns, so no second
			// client or copy of that state is needed.
			Status: ctx,
		})
	}

	// Bind before starting any background work: a port already in use must fail
	// startup instead of leaving a process that silently receives nothing.
	srv := server.New(server.Config{
		Addr:            fmt.Sprintf(":%d", ctx.Config.PDF.WebhookServerPort),
		TelegramWebhook: telegramUpdate,
		PDFCallback:     newWebhooker(ctx).WebhookHandler,
		Static:          staticHandler,
		API:             apiHandler,
		Logger:          ctx.Logger,
	})
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start HTTP server: %w", err)
	}
	ctx.httpServer = srv
	ctx.Logger.Info().Str("addr", srv.Addr()).Msg("HTTP server listening")

	// Telegram accepts updates through getUpdates or the webhook, never both.
	// Webhook mode publishes the URL in the background and consumes pushes;
	// polling mode first clears a leftover registration, otherwise Telegram
	// keeps pushing to it and getUpdates fails with 409 Conflict.
	if webhookMode {
		ctx.Logger.Info().Str("url", ctx.Config.WebhookURL()).Msg("starting in webhook mode")
		go ctx.registerWebhook()
		go ctx.Bot.StartWebhook(ctx.ctx)
	} else {
		ctx.Logger.Info().Msg("starting in polling mode")
		ctx.deleteWebhook()
		go ctx.Bot.Start(ctx.ctx)
	}

	scheduleInterval := config.ScheduleInterval()

	job, _ := ctx.scheduler.Every(scheduleInterval).Hours().Name("fetch_info").SingletonMode().Do(func() error {
		ctx.processor.Process()
		return nil
	})
	l := ctx.Logger.Debug()
	job.RegisterEventListeners(
		gocron.AfterJobRuns(func(jobName string) {
			l.Msgf("afterJobRuns: %s_scheduler", jobName)
		}),
		gocron.BeforeJobRuns(func(jobName string) {
			l.Msgf("beforeJobRuns: %s_scheduler", jobName)
		}),
		gocron.WhenJobReturnsError(func(jobName string, err error) {
			l.Msgf("whenJobReturnsError: %s_scheduler, %v", jobName, err)
		}),
		gocron.WhenJobReturnsNoError(func(jobName string) {
			l.Msgf("whenJobReturnsNoError: %s_scheduler", jobName)
		}),
	)

	ctx.scheduler.StartAsync()
	// Attachments that failed to convert are retried here, independently of the
	// crawl schedule: a Gotenberg restart would otherwise cost the whole window.
	if ctx.retrier != nil {
		go ctx.retrier.Start(ctx.ctx)
	}
	return nil
}

// Stop shuts the process down. The webhook registration is deliberately kept:
// Telegram retains undelivered updates for up to 24 hours and resumes delivery
// to the same URL, so deleting it would only add a registration gap.
func (ctx *BotContext) Stop() {
	if ctx.cancel != nil {
		ctx.cancel()
	}
	// Drain the listener first so no new update is accepted while the processor
	// and the scheduler are being released.
	if ctx.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()

		if err := ctx.httpServer.Shutdown(shutdownCtx); err != nil {
			ctx.Logger.Error().Err(err).Msg("HTTP server shutdown error")
		}
	}
	ctx.processor.Release()
	ctx.scheduler.Stop()
	ctx.scheduler.StopBlockingChan()
}

// registerWebhook publishes the webhook URL to Telegram and keeps retrying
// until it succeeds or the bot shuts down. It runs in the background so an
// unreachable Telegram API delays deliveries instead of startup. The call is
// idempotent (setWebhook replaces the same registration), so retrying one that
// actually succeeded but reported a timeout is harmless.
func (ctx *BotContext) registerWebhook() {
	params := &bot.SetWebhookParams{
		URL:            ctx.Config.WebhookURL(),
		SecretToken:    ctx.Config.WebhookSecret,
		AllowedUpdates: []string{"message", "callback_query", "edited_message"},
		MaxConnections: 40,
	}

	for ctx.context().Err() == nil {
		ok, err := ctx.Bot.SetWebhook(ctx.context(), params)
		switch {
		case err != nil && ctx.context().Err() != nil:
			// Shutdown aborted the request; that is not a registration failure.
			return
		case err != nil:
			ctx.Logger.Warn().Err(err).Str("url", params.URL).
				Dur("retryIn", webhookRetryInterval).
				Msg("telegram setWebhook failed")
		case !ok:
			ctx.Logger.Warn().Str("url", params.URL).
				Dur("retryIn", webhookRetryInterval).
				Msg("telegram setWebhook was not accepted")
		default:
			ctx.Logger.Info().Str("url", params.URL).Msg("telegram webhook registered")
			return
		}

		select {
		case <-ctx.context().Done():
			return
		case <-time.Tick(webhookRetryInterval):
		}
	}
}

// deleteWebhook clears a registration left by an earlier webhook run. It is
// best effort and bounded: polling cannot start while a webhook is set, but a
// failed cleanup must not keep the bot from trying anyway.
func (ctx *BotContext) deleteWebhook() {
	timeoutCtx, cancel := context.WithTimeout(ctx.context(), webhookSetupTimeout)
	defer cancel()

	if _, err := ctx.Bot.DeleteWebhook(timeoutCtx, &bot.DeleteWebhookParams{}); err != nil {
		ctx.Logger.Warn().Err(err).Msg("telegram deleteWebhook failed; a stale webhook may block getUpdates")
	}
}
