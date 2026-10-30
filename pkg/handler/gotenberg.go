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
	//
	// It must stay above Gotenberg's own request limit (API_TIMEOUT, 30s by
	// default): when the two deadlines coincide the client fires first and the
	// server's descriptive 504 never reaches the bot.
	gotenbergHTTPTimeout = 3 * time.Minute
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

// transportErrorKey carries a *transportError through a request context.
type transportErrorKey struct{}

// transportError remembers why the underlying round trip failed. The Gotenberg
// client replaces every transport failure with a fixed "request sending
// failed", so without this recorder the real cause — DNS, connection refused,
// TLS, timeout — would be lost before it reaches the logs.
type transportError struct {
	err error
}

// recordingTransport stores the transport error of the request it serves. The
// recorder is per request (carried by the context), so concurrent conversions
// cannot overwrite each other.
type recordingTransport struct {
	base http.RoundTripper
}

func (t recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	resp, err := base.RoundTrip(req)
	if err != nil {
		if holder, ok := req.Context().Value(transportErrorKey{}).(*transportError); ok && holder != nil {
			holder.err = err
		}
	}
	return resp, err
}

// withTransportRecorder attaches a recorder to ctx and returns it, so the
// caller can recover the underlying cause of a failed request.
func withTransportRecorder(ctx context.Context) (context.Context, *transportError) {
	failure := new(transportError)
	return context.WithValue(ctx, transportErrorKey{}, failure), failure
}

// describeTransportError prefers the recorded transport failure over the
// library's opaque placeholder.
func describeTransportError(failure *transportError, err error) error {
	if failure != nil && failure.err != nil {
		return failure.err
	}
	return err
}

func NewGotenbergClient(host, hookURL, token string) (*GotenbergClient, error) {
	client, err := gotenberg.NewClient(host, &http.Client{
		Timeout:   gotenbergHTTPTimeout,
		Transport: recordingTransport{base: http.DefaultTransport},
	})
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

	ctx, failure := withTransportRecorder(ctx)
	resp, err := g.client.Send(ctx, req)
	if err := checkGotenbergResponse("URLToPDF", resp, describeTransportError(failure, err)); err != nil {
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

	ctx, failure := withTransportRecorder(ctx)
	resp, err := g.client.Screenshot(ctx, req)
	if err := checkGotenbergResponse("HtmlToImage", resp, describeTransportError(failure, err)); err != nil {
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

	ctx, failure := withTransportRecorder(ctx)
	resp, err := g.client.Screenshot(ctx, req)
	if err := checkGotenbergResponse("URLToImage", resp, describeTransportError(failure, err)); err != nil {
		return "", err
	}
	return requestID, nil
}

// FetchPDF converts a URL to PDF and returns the file itself. Unlike URLToPDF
// it does not use a webhook, so it suits the push pipeline where the caller
// wants the bytes in hand before sending them as an attachment.
func (g *GotenbergClient) FetchPDF(ctx context.Context, u string) ([]byte, error) {
	started := time.Now()
	ctx, failure := withTransportRecorder(ctx)

	resp, err := g.client.Send(ctx, gotenberg.NewURLRequest(u))
	if err != nil {
		return nil, fmt.Errorf("convert %s failed after %s: %w",
			u, time.Since(started).Round(time.Millisecond), describeTransportError(failure, err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, gotenbergErrorBodyLimit))
		return nil, fmt.Errorf("gotenberg convert returned status %d after %s: %s",
			resp.StatusCode, time.Since(started).Round(time.Millisecond), strings.TrimSpace(string(body)))
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
