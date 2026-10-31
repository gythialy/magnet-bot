package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// initDataHeader is where the Mini App puts the raw, still URL-encoded
	// initData string it received from window.Telegram.WebApp.initData. The
	// value is a bearer credential, so it is never written to a log.
	initDataHeader = "X-Telegram-Init-Data"

	// webAppDataKey is the literal key Telegram uses for the outer HMAC of the
	// documented initData check. It is not configurable; changing it would
	// make every client signature invalid.
	webAppDataKey = "WebAppData"

	// defaultInitDataTTL is used when a caller passes a non-positive ttl. One
	// hour is the window agreed in the Mini App plan; callers that need a
	// different window pass it explicitly to ValidateInitData or the
	// middleware constructor.
	defaultInitDataTTL = time.Hour

	// maxInitDataClockSkew tolerates a phone clock that is slightly ahead of
	// the server. initData is signed on the device, so a small drift is
	// normal; a timestamp further in the future is treated as forgery rather
	// than a slow watch.
	maxInitDataClockSkew = 60 * time.Second

	// initDataHashField is the one field excluded from the signed string; its
	// value is the expected signature itself.
	initDataHashField = "hash"

	// initDataAuthDateField carries the signing time in unix seconds.
	initDataAuthDateField = "auth_date"

	// initDataUserField is the JSON-encoded Telegram user object.
	initDataUserField = "user"

	// mediaTypeJSON is used by every Mini App API response so clients can
	// parse without sniffing.
	mediaTypeJSON = "application/json; charset=utf-8"
)

var (
	// ErrUnauthorized reports initData that is missing, malformed, or not
	// signed by the configured bot. It is deliberately coarse for clients:
	// they must not learn which individual check failed.
	ErrUnauthorized = errors.New("init data is not authorized")

	// ErrExpired reports correctly signed initData whose auth_date is older
	// than the permitted ttl. Callers still answer 401, but may distinguish
	// it from ErrUnauthorized when logging or when asking the client to
	// reopen the Mini App.
	ErrExpired = errors.New("init data has expired")
)

// InitDataUser is the authenticated identity derived from initData. Only
// stable identity fields are exposed; the full Telegram user object carries
// more (photo_url, is_premium, ...) and none of it is needed to scope data.
type InitDataUser struct {
	ID           int64
	Username     string
	FirstName    string
	LastName     string
	LanguageCode string
}

// initDataUserPayload mirrors the JSON shape Telegram sends in the "user"
// field. It is separate from InitDataUser so that decoding tags never leak
// into the public identity type.
type initDataUserPayload struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	LanguageCode string `json:"language_code"`
}

// ValidateInitData verifies rawInitData exactly as Telegram documents it:
//
//	secret             = HMAC_SHA256(key="WebAppData", msg=botToken)
//	data_check_string  = all fields except "hash", sorted by key,
//	                     joined as "key=value" with "\n"
//	expected_hash      = hex(HMAC_SHA256(key=secret, msg=data_check_string))
//
// The raw string is already URL-encoded, so it is parsed with url.ParseQuery
// and the decoded values are signed over; that is what the official examples
// do, and any other choice makes genuine clients fail for values containing
// reserved characters (the user JSON always does).
//
// now is injected instead of reading the clock so callers and tests can pin
// time. A non-positive ttl falls back to defaultInitDataTTL (one hour).
//
// All failures are reported as ErrUnauthorized or ErrExpired. The wrapping is
// intentionally absent: callers match with errors.Is, and a richer error text
// would give an unauthenticated client a useful oracle.
func ValidateInitData(botToken, rawInitData string, now time.Time, ttl time.Duration) (InitDataUser, error) {
	// An empty token would mean HMACing with a publicly known key, so every
	// request would be forgeable. Fail closed instead of trusting config.
	if botToken == "" {
		return InitDataUser{}, ErrUnauthorized
	}

	values, err := url.ParseQuery(rawInitData)
	if err != nil {
		// ParseQuery also rejects semicolons and broken percent escapes; those
		// cannot be produced by Telegram's own encoder.
		return InitDataUser{}, ErrUnauthorized
	}

	// Read the signature through the map instead of Values.Get so a repeated
	// "hash" field is rejected: two signatures would make it ambiguous which
	// one the check string is supposed to authenticate.
	hashes := values[initDataHashField]
	if len(hashes) != 1 {
		return InitDataUser{}, ErrUnauthorized
	}
	providedHash := hashes[0]
	// A sha256 digest is exactly 64 hex characters. Checking the length first
	// keeps the decode below from being the only thing standing between a
	// malformed header and the constant-time comparison.
	if len(providedHash) != 2*sha256.Size {
		return InitDataUser{}, ErrUnauthorized
	}
	providedDigest, err := hex.DecodeString(providedHash)
	if err != nil {
		return InitDataUser{}, ErrUnauthorized
	}

	checkString := initDataCheckString(values)

	secretMAC := hmac.New(sha256.New, []byte(webAppDataKey))
	secretMAC.Write([]byte(botToken))
	expectedMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	expectedMAC.Write([]byte(checkString))

	// Constant time: a byte-by-byte early exit would let an attacker recover
	// the expected signature one position at a time.
	if !hmac.Equal(expectedMAC.Sum(nil), providedDigest) {
		return InitDataUser{}, ErrUnauthorized
	}

	// Only interpret fields after the signature matched: before that point the
	// timestamp and the identity are attacker-controlled strings, and the order
	// of the checks must not reveal anything about them.
	rawAuthDate := values.Get(initDataAuthDateField)
	if rawAuthDate == "" {
		return InitDataUser{}, ErrUnauthorized
	}
	authSeconds, err := strconv.ParseInt(rawAuthDate, 10, 64)
	if err != nil {
		return InitDataUser{}, ErrUnauthorized
	}
	authTime := time.Unix(authSeconds, 0)

	// Reject a timestamp that is implausibly far in the future first: it is a
	// forgery signal, not staleness, so it must not be reported as ErrExpired.
	if authTime.After(now.Add(maxInitDataClockSkew)) {
		return InitDataUser{}, ErrUnauthorized
	}
	if now.Sub(authTime) > effectiveInitDataTTL(ttl) {
		return InitDataUser{}, ErrExpired
	}

	return initDataUser(values)
}

// initDataCheckString builds the newline-joined, key-sorted payload that the
// client signature covers. Every occurrence of a duplicated key is included
// in value order so canonicalisation stays deterministic; Telegram itself
// sends each field once, so this only matters for hand-built inputs.
func initDataCheckString(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == initDataHashField {
			continue
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var builder strings.Builder
	first := true
	for _, key := range keys {
		for _, value := range values[key] {
			if !first {
				builder.WriteByte('\n')
			}
			first = false
			builder.WriteString(key)
			builder.WriteByte('=')
			builder.WriteString(value)
		}
	}
	return builder.String()
}

// initDataUser decodes the signed "user" field. A missing or unparsable user,
// or a non-positive id, is a protocol violation: every legitimate initData
// payload describes the user who opened the Mini App.
func initDataUser(values url.Values) (InitDataUser, error) {
	rawUser := values.Get(initDataUserField)
	if rawUser == "" {
		return InitDataUser{}, ErrUnauthorized
	}

	var payload initDataUserPayload
	if err := json.Unmarshal([]byte(rawUser), &payload); err != nil {
		return InitDataUser{}, ErrUnauthorized
	}
	if payload.ID <= 0 {
		return InitDataUser{}, ErrUnauthorized
	}

	// The payload mirrors InitDataUser field-for-field, so a direct conversion
	// keeps the two definitions visibly in lockstep: adding a field to one
	// without the other stops compiling instead of silently dropping data.
	return InitDataUser(payload), nil
}

// effectiveInitDataTTL turns a missing (zero) or negative window into the
// documented default so a misconfigured caller does not reject every request.
func effectiveInitDataTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultInitDataTTL
	}
	return ttl
}

// NewInitDataMiddleware returns middleware that authenticates every request
// below /api/* and injects the resulting InitDataUser into the request
// context. The middleware holds no mutable state, so one instance can wrap the
// whole API subtree safely.
//
// A missing or invalid header produces 401 with the shared error envelope.
// Expiry is also a 401 for the client, but the server log records the reason.
// The raw header value, the bot token and the signature are never logged: the
// header is a bearer credential and a log sink is often less protected than
// the request channel.
func NewInitDataMiddleware(botToken string, ttl time.Duration) func(http.Handler) http.Handler {
	// Resolve the window once: it is constant for the handler lifetime and
	// this keeps the request path free of repeated normalisation.
	freshness := effectiveInitDataTTL(ttl)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawInitData := r.Header.Get(initDataHeader)
			if rawInitData == "" {
				logInitDataRejection(r, "missing header")
				WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			user, err := ValidateInitData(botToken, rawInitData, time.Now(), freshness)
			if err != nil {
				logInitDataRejection(r, initDataRejectionReason(err))
				// Both ErrExpired and ErrUnauthorized surface as 401: the
				// client cannot repair either condition except by reopening
				// the Mini App, and the distinction stays server-side.
				WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			next.ServeHTTP(w, r.WithContext(withInitDataUser(r.Context(), user)))
		})
	}
}

// initDataRejectionReason maps a validation sentinel to a stable log phrase.
// Fixed phrases are used instead of passing err.Error() through, so a future
// wrapped error can never smuggle request data into the log line.
func initDataRejectionReason(err error) string {
	if errors.Is(err, ErrExpired) {
		return "expired"
	}
	return "signature or payload invalid"
}

// logInitDataRejection records one line per rejected request. Only the method,
// the path and a fixed reason are logged: the header, query string and body
// may all carry credentials, and the path is already logged by the transport.
func logInitDataRejection(r *http.Request, reason string) {
	slog.Warn("mini app init data rejected",
		"method", r.Method,
		"path", r.URL.Path,
		"reason", reason,
	)
}

// apiErrorResponse is the single error envelope served by the Mini App API, so
// clients only need one shape: {"error":{"code":"..."}}.
type apiErrorResponse struct {
	Error apiErrorDetail `json:"error"`
}

type apiErrorDetail struct {
	Code string `json:"code"`
}

// internalErrorBody is pre-encoded because WriteJSON reaches it only after
// json.Marshal already failed, and marshalling this literal could not fix a
// broken caller value.
var internalErrorBody = []byte(`{"error":{"code":"internal_error"}}`)

// WriteJSON writes v as a JSON response with the given status. The body is
// marshalled before the status is committed, so an encoding failure still
// yields a well-formed JSON 500 instead of a truncated success response.
//
// It deliberately does not append a trailing newline: handlers compare and
// tests assert on the exact body, and the error envelope is a fixed string.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("encode mini app response", "error", err)
		w.Header().Set("Content-Type", mediaTypeJSON)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(internalErrorBody)
		return
	}

	w.Header().Set("Content-Type", mediaTypeJSON)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// WriteError answers with the standard Mini App error envelope, for example
// WriteError(w, http.StatusUnauthorized, "unauthorized") writes exactly
// {"error":{"code":"unauthorized"}}.
func WriteError(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, apiErrorResponse{Error: apiErrorDetail{Code: code}})
}
