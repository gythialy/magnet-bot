package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gythialy/magnet/pkg/config"
	"github.com/gythialy/magnet/pkg/model"
	"github.com/gythialy/magnet/pkg/utils"
	"github.com/rs/zerolog"
)

func newTestWebhooker() *webhooker {
	nopLogger := zerolog.Nop()
	return newWebhooker(&BotContext{
		Logger: &utils.Logger{Logger: &nopLogger},
		Config: &config.ServiceConfig{PDF: &config.PDFServiceConfig{}},
		Store:  NewStore[model.RequestInfo](),
	})
}

// TestWebhookRouting pins the route pattern: the request id is a path value and
// an unknown id is rejected before the body is read.
func TestWebhookRouting(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /pdf/{id}", newTestWebhooker().WebhookHandler)

	t.Run("unknown request id", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/pdf/unknown", strings.NewReader("payload"))
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pdf/unknown", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})

	t.Run("token mismatch", func(t *testing.T) {
		wh := newTestWebhooker()
		wh.ctx.Config.PDF.WebhookToken = "secret"
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/pdf/unknown", strings.NewReader("payload"))
		wh.WebhookHandler(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})
}
