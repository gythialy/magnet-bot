package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/gythialy/magnet/pkg/constant"
)

type PDFServiceConfig struct {
	WebhookServer     string
	WebhookServerPort int
	WebhookToken      string
	PDFServiceURL     string
}

func (c *PDFServiceConfig) Init() *PDFServiceConfig {
	if u := os.Getenv(constant.PDFServerUrl); u != "" {
		c.PDFServiceURL = u
	}
	if u := os.Getenv(constant.WebhookServerURL); u != "" {
		c.WebhookServer = u
	}

	if v := os.Getenv(constant.WebhookServerPort); v != "" {
		c.WebhookServerPort, _ = strconv.Atoi(v)
	}
	c.WebhookToken = os.Getenv(constant.WebhookToken)
	return c
}

func (c *PDFServiceConfig) WebhookURL() string {
	return fmt.Sprintf("%s:%d", c.WebhookServer, c.WebhookServerPort)
}

type ServiceConfig struct {
	PDF              *PDFServiceConfig
	ManagerId        int64
	MessageServerUrl string
	BaseDir          string
	LogLevel         zerolog.Level

	// PublicBaseURL is the externally reachable origin, e.g.
	// "https://bot.example.com". It already carries the scheme, so the URL
	// helpers below only append paths. Empty means the bot stays in polling
	// mode and exposes nothing publicly.
	PublicBaseURL          string
	WebhookSecret          string
	TelegramWebhookEnabled bool
	MiniAppEnabled         bool
}

func NewServiceConfig() *ServiceConfig {
	pdf := &PDFServiceConfig{}
	return &ServiceConfig{
		PDF:                    pdf.Init(),
		ManagerId:              ManagerId(),
		MessageServerUrl:       MessageServerUrl(),
		BaseDir:                BaseDir(),
		LogLevel:               LogLevel(),
		PublicBaseURL:          PublicBaseURL(),
		WebhookSecret:          TelegramWebhookSecret(),
		TelegramWebhookEnabled: TelegramWebhookEnabled(),
		MiniAppEnabled:         MiniAppEnabled(),
	}
}

// webhookSecretPattern is Telegram's documented alphabet and length for
// secret_token. Checking it at startup avoids a config that would make every
// incoming webhook fail the header check after deployment.
var webhookSecretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

// Validate checks the public-facing settings and normalises PublicBaseURL in
// place. It is separate from NewServiceConfig so callers can decide how to
// report a bad configuration instead of forcing a panic during construction.
//
// An empty PUBLIC_BASE_URL always validates: that is the legacy polling mode.
// MiniAppEnabled deliberately does not require PUBLIC_BASE_URL, because the
// Mini App is also useful against a local reverse proxy while debugging.
func (c *ServiceConfig) Validate() error {
	if c.PublicBaseURL == "" {
		return nil
	}

	u, err := url.Parse(c.PublicBaseURL)
	if err != nil {
		return fmt.Errorf("PUBLIC_BASE_URL must be a valid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("PUBLIC_BASE_URL must use https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("PUBLIC_BASE_URL must include a host")
	}
	c.PublicBaseURL = strings.TrimRight(c.PublicBaseURL, "/")

	// Without the webhook there is nothing for the secret or the HTTP port to
	// protect, so polling mode does not need either of them.
	if !c.TelegramWebhookEnabled {
		return nil
	}
	if c.WebhookSecret == "" {
		return errors.New("TELEGRAM_WEBHOOK_SECRET is required when TELEGRAM_WEBHOOK_ENABLED is true")
	}
	if !webhookSecretPattern.MatchString(c.WebhookSecret) {
		return errors.New("TELEGRAM_WEBHOOK_SECRET must be 1-256 characters of [A-Za-z0-9_-]")
	}
	if c.PDF == nil || c.PDF.WebhookServerPort == 0 {
		return errors.New("WEBHOOK_SERVER_PORT is required when TELEGRAM_WEBHOOK_ENABLED is true")
	}
	return nil
}

// PublicEnabled reports whether a public base URL is configured. It does not
// prove the URL is valid; call Validate before serving.
func (c *ServiceConfig) PublicEnabled() bool {
	return c.PublicBaseURL != ""
}

// WebhookURL returns the Telegram webhook endpoint, or "" when no public entry
// point is configured.
func (c *ServiceConfig) WebhookURL() string {
	return publicURL(c.PublicBaseURL, constant.TelegramWebhookPath)
}

// AppURL returns the Mini App page URL, or "" when no public entry point is
// configured.
func (c *ServiceConfig) AppURL() string {
	return publicURL(c.PublicBaseURL, constant.MiniAppPath)
}

// APIBaseURL returns the Mini App API prefix, or "" when no public entry point
// is configured.
func (c *ServiceConfig) APIBaseURL() string {
	return publicURL(c.PublicBaseURL, constant.APIPath)
}

// HealthURL returns the health check URL, or "" when no public entry point is
// configured.
func (c *ServiceConfig) HealthURL() string {
	return publicURL(c.PublicBaseURL, constant.HealthPath)
}

// publicURL joins a base URL and a fixed path. Trailing slashes are removed
// from the base so a configured "https://host/" and "https://host" produce the
// same URLs.
func publicURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return ""
	}
	return base + path
}
