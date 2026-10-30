package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dcaraxes/gotenberg-go-client/v8"
	"github.com/dcaraxes/gotenberg-go-client/v8/document"
	"github.com/google/uuid"
	"github.com/gythialy/magnet/pkg/constant"
)

const (
	fname = "index.html"

	// gotenbergHTTPTimeout bounds the HTTP client used for PDF/IMG conversion
	// requests. Without a timeout a hung Gotenberg instance would leave the
	// conversion goroutine and its webhook request state stuck forever.
	gotenbergHTTPTimeout = 2 * time.Minute
	// gotenbergErrorBodyLimit caps how much of an error response is quoted.
	gotenbergErrorBodyLimit = 4 << 10
	// maxAttachmentBytes keeps an attachment inside Telegram's 50 MB bot limit.
	maxAttachmentBytes = 45 << 20
)

type GotenbergClient struct {
	client  *gotenberg.Client
	hookURL string
	token   string
}

func NewGotenbergClient(host, hookURL, token string) (*GotenbergClient, error) {
	client, err := gotenberg.NewClient(host, &http.Client{Timeout: gotenbergHTTPTimeout})
	if err != nil {
		return nil, err
	}
	return &GotenbergClient{client: client, hookURL: hookURL, token: token}, nil
}

// newWebhook builds the callback URL of one conversion request and returns the
// request id that identifies it.
func (g *GotenbergClient) newWebhook() (requestID, hookURL string) {
	requestID = uuid.New().String()
	hookURL = g.hookURL + constant.PDFEndPoint + requestID
	if g.token != "" {
		hookURL += "?token=" + url.QueryEscape(g.token)
	}
	return requestID, hookURL
}

// URLToPDF asks Gotenberg to convert a URL and deliver the PDF to the webhook.
func (g *GotenbergClient) URLToPDF(ctx context.Context, u string) (string, error) {
	requestID, hookURL := g.newWebhook()
	req := gotenberg.NewURLRequest(u)
	req.SetWebhookMethod(http.MethodPost)
	req.UseWebhook(hookURL, hookURL)

	resp, err := g.client.Send(ctx, req)
	if err := checkGotenbergResponse("URLToPDF", resp, err); err != nil {
		return "", err
	}
	return requestID, nil
}

// HtmlToImage asks Gotenberg to screenshot inline HTML and deliver it to the
// webhook.
func (g *GotenbergClient) HtmlToImage(ctx context.Context, content string) (string, error) {
	index, err := document.FromString(fname, content)
	if err != nil {
		return "", err
	}

	requestID, hookURL := g.newWebhook()
	req := gotenberg.NewHTMLRequest(index)
	req.ScreenshotOptimizeForSpeed()
	req.SetWebhookMethod(http.MethodPost)
	req.UseWebhook(hookURL, hookURL)

	resp, err := g.client.Screenshot(ctx, req)
	if err := checkGotenbergResponse("HtmlToImage", resp, err); err != nil {
		return "", err
	}
	return requestID, nil
}

// URLToImage asks Gotenberg to screenshot a URL and deliver it to the webhook.
func (g *GotenbergClient) URLToImage(ctx context.Context, u string) (string, error) {
	requestID, hookURL := g.newWebhook()
	req := gotenberg.NewURLRequest(u)
	req.EmulateScreenMediaType()
	req.SetWebhookMethod(http.MethodPost)
	req.UseWebhook(hookURL, hookURL)

	resp, err := g.client.Screenshot(ctx, req)
	if err := checkGotenbergResponse("URLToImage", resp, err); err != nil {
		return "", err
	}
	return requestID, nil
}

// FetchPDF converts a URL to PDF and returns the file itself. Unlike URLToPDF
// it does not use a webhook, so it suits the push pipeline where the caller
// wants the bytes in hand before sending them as an attachment.
func (g *GotenbergClient) FetchPDF(ctx context.Context, u string) ([]byte, error) {
	resp, err := g.client.Send(ctx, gotenberg.NewURLRequest(u))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, gotenbergErrorBodyLimit))
		return nil, fmt.Errorf("gotenberg convert returned status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAttachmentBytes {
		return nil, fmt.Errorf("converted PDF is larger than %d bytes", maxAttachmentBytes)
	}
	return data, nil
}

// checkGotenbergResponse closes the response body and turns any status other
// than 204 No Content into an error. 204 means the webhook was accepted.
func checkGotenbergResponse(operation string, resp *http.Response, err error) error {
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("gotenberg %s returned no response", operation)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, gotenbergErrorBodyLimit))
	return fmt.Errorf("gotenberg %s returned status %d: %s", operation, resp.StatusCode, strings.TrimSpace(string(body)))
}
