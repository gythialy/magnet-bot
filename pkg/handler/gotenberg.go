package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/dcaraxes/gotenberg-go-client/v8"
	"github.com/dcaraxes/gotenberg-go-client/v8/document"
	"github.com/google/uuid"
	"github.com/gythialy/magnet/pkg/constant"
)

const fname = "index.html"

type GotenbergClient struct {
	client  *gotenberg.Client
	hookURL string
}

func NewGotenbergClient(host string, hookURL string) (*GotenbergClient, error) {
	if client, err := gotenberg.NewClient(host, http.DefaultClient); err == nil {
		return &GotenbergClient{
			client:  client,
			hookURL: hookURL,
		}, nil
	} else {
		return nil, err
	}
}

func (g *GotenbergClient) URLToPDF(u string) (string, error) {
	req := gotenberg.NewURLRequest(u)
	req.SetWebhookMethod(http.MethodPost)
	requestId := uuid.New().String()
	hookURL := fmt.Sprintf("%s%s%s", g.hookURL, constant.PDFEndPoint, requestId)
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
