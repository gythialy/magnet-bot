package config

import (
	"strings"
	"testing"

	"github.com/gythialy/magnet/pkg/constant"
)

// clearPublicEnv removes the public-entry variables so tests do not depend on
// the shell that started them.
func clearPublicEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		constant.PublicBaseURL,
		constant.TelegramWebhookSecret,
		constant.TelegramWebhookEnabled,
		constant.MiniAppEnabled,
	} {
		t.Setenv(key, "")
	}
}

// TestNewServiceConfigPublicDefaults guards the two on-by-default switches and
// the polling-mode fallback when no public variables are set.
func TestNewServiceConfigPublicDefaults(t *testing.T) {
	clearPublicEnv(t)

	cfg := NewServiceConfig()
	if cfg.PublicBaseURL != "" {
		t.Errorf("PublicBaseURL = %q, want empty", cfg.PublicBaseURL)
	}
	if cfg.WebhookSecret != "" {
		t.Errorf("WebhookSecret = %q, want empty", cfg.WebhookSecret)
	}
	if !cfg.TelegramWebhookEnabled {
		t.Error("TelegramWebhookEnabled = false, want true by default")
	}
	if !cfg.MiniAppEnabled {
		t.Error("MiniAppEnabled = false, want true by default")
	}
	if cfg.PublicEnabled() {
		t.Error("PublicEnabled() = true, want false when PUBLIC_BASE_URL is unset")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with no variables set = %v, want nil", err)
	}
}

// TestNewServiceConfigPublicFromEnv checks that the constructor reads the new
// variables and normalises the trailing slash.
func TestNewServiceConfigPublicFromEnv(t *testing.T) {
	clearPublicEnv(t)
	t.Setenv(constant.PublicBaseURL, "https://bot.example.com/")
	t.Setenv(constant.TelegramWebhookSecret, "abc-123_XYZ")
	t.Setenv(constant.TelegramWebhookEnabled, "off")
	t.Setenv(constant.MiniAppEnabled, "0")

	cfg := NewServiceConfig()
	if cfg.PublicBaseURL != "https://bot.example.com" {
		t.Errorf("PublicBaseURL = %q, want %q", cfg.PublicBaseURL, "https://bot.example.com")
	}
	if cfg.WebhookSecret != "abc-123_XYZ" {
		t.Errorf("WebhookSecret = %q, want %q", cfg.WebhookSecret, "abc-123_XYZ")
	}
	if cfg.TelegramWebhookEnabled {
		t.Error("TelegramWebhookEnabled = true, want false")
	}
	if cfg.MiniAppEnabled {
		t.Error("MiniAppEnabled = true, want false")
	}
}

func TestServiceConfigValidate(t *testing.T) {
	validPDF := &PDFServiceConfig{WebhookServerPort: 8080}

	tests := []struct {
		name    string
		cfg     ServiceConfig
		wantErr string // substring; empty means Validate must succeed
	}{
		{
			name: "https with webhook settings",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com",
				WebhookSecret:          "s3cr3t_Value-1",
				TelegramWebhookEnabled: true,
				MiniAppEnabled:         true,
				PDF:                    validPDF,
			},
		},
		{
			name: "https with trailing slash",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com/",
				WebhookSecret:          "s3cr3t_Value-1",
				TelegramWebhookEnabled: true,
				PDF:                    validPDF,
			},
		},
		{
			name: "secret at maximum length",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com",
				WebhookSecret:          strings.Repeat("a", 256),
				TelegramWebhookEnabled: true,
				PDF:                    validPDF,
			},
		},
		{
			name: "webhook disabled skips secret and port",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com",
				TelegramWebhookEnabled: false,
			},
		},
		{
			name: "mini app without public base URL is allowed",
			cfg: ServiceConfig{
				MiniAppEnabled: true,
			},
		},
		{
			name: "http is rejected",
			cfg: ServiceConfig{
				PublicBaseURL: "http://bot.example.com",
			},
			wantErr: "PUBLIC_BASE_URL must use https",
		},
		{
			name: "scheme is required",
			cfg: ServiceConfig{
				PublicBaseURL: "bot.example.com",
			},
			wantErr: "PUBLIC_BASE_URL must use https",
		},
		{
			name: "malformed URL is rejected",
			cfg: ServiceConfig{
				PublicBaseURL: "https://exa mple.com",
			},
			wantErr: "PUBLIC_BASE_URL must be a valid URL",
		},
		{
			name: "host is required",
			cfg: ServiceConfig{
				PublicBaseURL: "https://",
			},
			wantErr: "PUBLIC_BASE_URL must include a host",
		},
		{
			name: "secret is required with webhook",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com",
				TelegramWebhookEnabled: true,
				PDF:                    validPDF,
			},
			wantErr: "TELEGRAM_WEBHOOK_SECRET is required",
		},
		{
			name: "secret alphabet is enforced",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com",
				WebhookSecret:          "not a valid secret!",
				TelegramWebhookEnabled: true,
				PDF:                    validPDF,
			},
			wantErr: "TELEGRAM_WEBHOOK_SECRET must be 1-256 characters",
		},
		{
			name: "secret length is enforced",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com",
				WebhookSecret:          strings.Repeat("a", 257),
				TelegramWebhookEnabled: true,
				PDF:                    validPDF,
			},
			wantErr: "TELEGRAM_WEBHOOK_SECRET must be 1-256 characters",
		},
		{
			name: "webhook port is required",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com",
				WebhookSecret:          "s3cr3t_Value-1",
				TelegramWebhookEnabled: true,
				PDF:                    &PDFServiceConfig{},
			},
			wantErr: "WEBHOOK_SERVER_PORT is required",
		},
		{
			name: "nil PDF config counts as missing port",
			cfg: ServiceConfig{
				PublicBaseURL:          "https://bot.example.com",
				WebhookSecret:          "s3cr3t_Value-1",
				TelegramWebhookEnabled: true,
			},
			wantErr: "WEBHOOK_SERVER_PORT is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestServiceConfigValidateNormalisesBaseURL(t *testing.T) {
	cfg := &ServiceConfig{
		PublicBaseURL:          "https://bot.example.com///",
		WebhookSecret:          "s3cr3t_Value-1",
		TelegramWebhookEnabled: true,
		PDF:                    &PDFServiceConfig{WebhookServerPort: 8080},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if cfg.PublicBaseURL != "https://bot.example.com" {
		t.Errorf("PublicBaseURL = %q, want %q", cfg.PublicBaseURL, "https://bot.example.com")
	}
	if got, want := cfg.WebhookURL(), "https://bot.example.com/telegram/webhook"; got != want {
		t.Errorf("WebhookURL() = %q, want %q", got, want)
	}
}

func TestServiceConfigURLs(t *testing.T) {
	tests := []struct {
		name        string
		base        string
		wantEnabled bool
		wantWebhook string
		wantApp     string
		wantAPI     string
		wantHealth  string
	}{
		{
			name:        "unset",
			base:        "",
			wantEnabled: false,
			wantWebhook: "",
			wantApp:     "",
			wantAPI:     "",
			wantHealth:  "",
		},
		{
			name:        "without trailing slash",
			base:        "https://bot.example.com",
			wantEnabled: true,
			wantWebhook: "https://bot.example.com/telegram/webhook",
			wantApp:     "https://bot.example.com/app/",
			wantAPI:     "https://bot.example.com/api/",
			wantHealth:  "https://bot.example.com/healthz",
		},
		{
			name:        "with trailing slash",
			base:        "https://bot.example.com/",
			wantEnabled: true,
			wantWebhook: "https://bot.example.com/telegram/webhook",
			wantApp:     "https://bot.example.com/app/",
			wantAPI:     "https://bot.example.com/api/",
			wantHealth:  "https://bot.example.com/healthz",
		},
		{
			name:        "with multiple trailing slashes",
			base:        "https://bot.example.com///",
			wantEnabled: true,
			wantWebhook: "https://bot.example.com/telegram/webhook",
			wantApp:     "https://bot.example.com/app/",
			wantAPI:     "https://bot.example.com/api/",
			wantHealth:  "https://bot.example.com/healthz",
		},
		{
			name:        "with explicit port",
			base:        "https://bot.example.com:8443",
			wantEnabled: true,
			wantWebhook: "https://bot.example.com:8443/telegram/webhook",
			wantApp:     "https://bot.example.com:8443/app/",
			wantAPI:     "https://bot.example.com:8443/api/",
			wantHealth:  "https://bot.example.com:8443/healthz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ServiceConfig{PublicBaseURL: tt.base}
			if got := cfg.PublicEnabled(); got != tt.wantEnabled {
				t.Errorf("PublicEnabled() = %t, want %t", got, tt.wantEnabled)
			}
			if got := cfg.WebhookURL(); got != tt.wantWebhook {
				t.Errorf("WebhookURL() = %q, want %q", got, tt.wantWebhook)
			}
			if got := cfg.AppURL(); got != tt.wantApp {
				t.Errorf("AppURL() = %q, want %q", got, tt.wantApp)
			}
			if got := cfg.APIBaseURL(); got != tt.wantAPI {
				t.Errorf("APIBaseURL() = %q, want %q", got, tt.wantAPI)
			}
			if got := cfg.HealthURL(); got != tt.wantHealth {
				t.Errorf("HealthURL() = %q, want %q", got, tt.wantHealth)
			}
		})
	}
}
