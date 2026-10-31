package handler

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
)

const (
	// telegramSecretHeader carries the secret_token configured through
	// setWebhook. Telegram sends it with every webhook delivery.
	telegramSecretHeader = "X-Telegram-Bot-Api-Secret-Token"
	// telegramWebhookMaxBodyBytes bounds the body of one update. Telegram
	// updates stay far below 1 MB even with a document attachment, so the limit
	// rejects abuse without ever truncating a legitimate delivery.
	telegramWebhookMaxBodyBytes = 1 << 20
)

// telegramWebhook wraps the bot library's webhook handler with the two checks
// the library leaves to its caller.
//
// The library answers a secret mismatch with 200 after logging only, because it
// assumes Telegram is the only caller; Telegram therefore treats the delivery
// as successful. The 403 has to happen here, before the body is touched, so an
// unauthorised caller cannot make the process buffer a megabyte.
//
// The accepted body is buffered once and put back on the request: the 1 MB
// limit can only be enforced after reading the bytes, and the library handler
// must still see them. Handing the same bytes back means the update is decoded
// from one read of the wire instead of a second trip through the socket.
func (ctx *BotContext) telegramWebhook(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if secret := ctx.Config.WebhookSecret; secret != "" &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get(telegramSecretHeader)), []byte(secret)) != 1 {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, telegramWebhookMaxBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			// MaxBytesReader is the only expected failure here, so anything
			// else (a client that hung up mid-body) is reported as a malformed
			// request rather than a size problem.
			if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		next(w, r)
	}
}
