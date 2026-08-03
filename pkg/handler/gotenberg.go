package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dcaraxes/gotenberg-go-client/v8"
	"github.com/dcaraxes/gotenberg-go-client/v8/document"
	"github.com/google/uuid"
	"github.com/gythialy/magnet/pkg/constant"
)

const fname = "index.html"

// gotenbergHTTPTimeout bounds the HTTP client used for PDF/IMG conversion
// requests. Without a timeout a hung Gotenberg instance would leave the
// conversion goroutine and its webhook request state stuck forever.
const gotenbergHTTPTimeout = 2 * time.Minute

type GotenbergClient struct {
	client  *gotenberg.Client
	hookURL string
	token   string
}

func NewGotenbergClient(host string, hookURL string, token string) (*GotenbergClient, error) {
	httpClient := &http.Client{Timeout: gotenbergHTTPTimeout}
	if client, err := gotenberg.NewClient(host, httpClient); err == nil {
		return &GotenbergClient{
			client:  client,
			hookURL: hookURL,
			token:   token,
		}, nil
	} else {
		return nil, err
	}
}

func (g *GotenbergClient) webhookToken() string {
	return g.token
}

func (g *GotenbergClient) URLToPDF(u string) (string, error) {
	req := gotenberg.NewURLRequest(u)
	req.SetWebhookMethod(http.MethodPost)
	requestId := uuid.New().String()
	hookURL := fmt.Sprintf("%s%s%s", g.hookURL, constant.PDFEndPoint, requestId)
	if token := g.webhookToken(); token != "" {
		hookURL = fmt.Sprintf("%s?token=%s", hookURL, token)
	}
	req.UseWebhook(hookURL, hookURL)

	if resp, err := g.client.Send(context.Background(), req); err == nil {
		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("gotenberg URLToPDF returned status: %d, Response body: %s",
				resp.StatusCode, string(body))
		} else {
			return requestId, nil
		}
	} else {
		return "", err
	}
}

func (g *GotenbergClient) HtmlToImage(content string) (string, error) {
	index, docErr := document.FromString(fname, content)
	if docErr != nil {
		return "", docErr
	}

	requestId := uuid.New().String()
	hookURL := fmt.Sprintf("%s%s%s", g.hookURL, constant.PDFEndPoint, requestId)
	if token := g.webhookToken(); token != "" {
		hookURL = fmt.Sprintf("%s?token=%s", hookURL, token)
	}
	req := gotenberg.NewHTMLRequest(index)
	req.ScreenshotOptimizeForSpeed()
	req.SetWebhookMethod(http.MethodPost)
	req.UseWebhook(hookURL, hookURL)

	if resp, err := g.client.Screenshot(context.Background(), req); err == nil {
		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("gotenberg HtmlToImage returned status: %d, Response body: %s",
				resp.StatusCode, string(body))
		} else {
			return requestId, nil
		}
	} else {
		return "", err
	}
}

func (g *GotenbergClient) URLToImage(u string) (string, error) {
	requestId := uuid.New().String()
	hookURL := fmt.Sprintf("%s%s%s", g.hookURL, constant.PDFEndPoint, requestId)
	if token := g.webhookToken(); token != "" {
		hookURL = fmt.Sprintf("%s?token=%s", hookURL, token)
	}
	req := gotenberg.NewURLRequest(u)
	req.EmulateScreenMediaType()
	req.SetWebhookMethod(http.MethodPost)
	req.UseWebhook(hookURL, hookURL)

	if resp, err := g.client.Screenshot(context.Background(), req); err == nil {
		if resp.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("gotenberg HtmlToImage returned status: %d, Response body: %s",
				resp.StatusCode, string(body))
		} else {
			return requestId, nil
		}
	} else {
		return "", err
	}
}
