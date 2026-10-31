package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// testBotToken is synthetic. No test below needs a real token or a network
	// connection: the signature is computed locally.
	testBotToken = "10001:TEST-TOKEN"
	// testUserID is the Telegram user id embedded in the test payloads.
	testUserID = int64(10001)
	// testNowUnix pins the clock so auth_date boundaries are exact.
	testNowUnix = int64(1700000000)
	// testQueryID is an arbitrary but realistic WebApp query id.
	testQueryID = "AAHdF6IQAAAAAN0XohDhrOrc"
)

// fixedInitDataHash is the expected signature for fixedFields() under
// testBotToken. It was produced outside this repository from Telegram's
// published algorithm (and cross-checked with a second implementation), so it
// is a known-answer test rather than a snapshot of our own output.
const fixedInitDataHash = "c0d1b22193e9ec42bc9c9a411bd7b3f268a73932105731905998c6c19ab45a01"

// fixedInitDataRaw is the same payload in the exact URL-encoded wire form a
// Mini App would send. Keeping it literal proves the decoder and the signer
// agree on percent-decoding, not just on in-memory strings.
const fixedInitDataRaw = "auth_date=1700000000&query_id=AAHdF6IQAAAAAN0XohDhrOrc&user=%7B%22id%22%3A10001%2C%22first_name%22%3A%22Test%22%2C%22last_name%22%3A%22User%22%2C%22username%22%3A%22tester%22%2C%22language_code%22%3A%22en%22%7D&hash=" + fixedInitDataHash

// fixedCheckString is the canonical signed payload for fixedFields(): fields
// sorted by key, joined with "=" and newlines, with the decoded user JSON.
const fixedCheckString = "auth_date=1700000000\n" +
	"query_id=AAHdF6IQAAAAAN0XohDhrOrc\n" +
	`user={"id":10001,"first_name":"Test","last_name":"User","username":"tester","language_code":"en"}`

// initDataField is one decoded initData key/value pair used by the test
// signer. The value is the decoded form, which is what the signature covers.
type initDataField struct {
	key   string
	value string
}

// independentInitDataHash re-implements Telegram's published algorithm from
// scratch: secret = HMAC("WebAppData", token), signature = HMAC(secret,
// sorted "k=v" lines). It deliberately does not call production code, so a
// mistake shared by implementation and test cannot hide.
func independentInitDataHash(botToken string, fields []initDataField) string {
	sorted := slices.Clone(fields)
	slices.SortStableFunc(sorted, func(a, b initDataField) int {
		return strings.Compare(a.key, b.key)
	})

	var checkString strings.Builder
	for i, field := range sorted {
		if i > 0 {
			checkString.WriteByte('\n')
		}
		checkString.WriteString(field.key)
		checkString.WriteByte('=')
		checkString.WriteString(field.value)
	}

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	secretMAC.Write([]byte(botToken))
	digestMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	digestMAC.Write([]byte(checkString.String()))

	return hex.EncodeToString(digestMAC.Sum(nil))
}

// encodeInitData renders pairs the way a Mini App serialises initData:
// percent-encoded key=value joined by "&", with the signature appended.
func encodeInitData(fields []initDataField, hash string) string {
	parts := make([]string, 0, len(fields)+1)
	for _, field := range fields {
		parts = append(parts, url.QueryEscape(field.key)+"="+url.QueryEscape(field.value))
	}
	parts = append(parts, "hash="+hash)
	return strings.Join(parts, "&")
}

// signInitData returns a correctly signed raw initData string plus the
// signature, signed with the independent test signer.
func signInitData(botToken string, fields []initDataField) (raw, hash string) {
	hash = independentInitDataHash(botToken, fields)
	return encodeInitData(fields, hash), hash
}

// testUserJSON renders the "user" field the way Telegram does.
func testUserJSON(id int64, username string) string {
	return fmt.Sprintf(
		`{"id":%d,"first_name":"Test","last_name":"User","username":%q,"language_code":"en"}`,
		id, username,
	)
}

// baseFields is a realistic payload whose declaration order is deliberately
// not sorted, so every test also exercises the signature's key sorting.
func baseFields(authUnix int64) []initDataField {
	return []initDataField{
		{key: "query_id", value: testQueryID},
		{key: "user", value: testUserJSON(testUserID, "tester")},
		{key: "auth_date", value: strconv.FormatInt(authUnix, 10)},
	}
}

func fixedFields() []initDataField {
	return []initDataField{
		{key: "auth_date", value: "1700000000"},
		{key: "query_id", value: testQueryID},
		{
			key:   "user",
			value: `{"id":10001,"first_name":"Test","last_name":"User","username":"tester","language_code":"en"}`,
		},
	}
}

func withoutField(fields []initDataField, key string) []initDataField {
	kept := make([]initDataField, 0, len(fields))
	for _, field := range fields {
		if field.key != key {
			kept = append(kept, field)
		}
	}
	return kept
}

func withField(fields []initDataField, key, value string) []initDataField {
	replaced := slices.Clone(fields)
	for i := range replaced {
		if replaced[i].key == key {
			replaced[i].value = value
			return replaced
		}
	}
	return append(replaced, initDataField{key: key, value: value})
}

// flipLastHexDigit changes exactly one signature character, which is what a
// byte-level forgery looks like.
func flipLastHexDigit(hash string) string {
	last := hash[len(hash)-1]
	replacement := byte('a')
	if last == 'a' {
		replacement = 'b'
	}
	return hash[:len(hash)-1] + string(replacement)
}

func TestValidateInitDataAcceptsSignedIdentity(t *testing.T) {
	now := time.Unix(testNowUnix, 0)
	raw, _ := signInitData(testBotToken, baseFields(testNowUnix))

	user, err := ValidateInitData(testBotToken, raw, now, time.Hour)
	if err != nil {
		t.Fatalf("ValidateInitData: %v", err)
	}

	want := InitDataUser{
		ID:           testUserID,
		Username:     "tester",
		FirstName:    "Test",
		LastName:     "User",
		LanguageCode: "en",
	}
	if user != want {
		t.Fatalf("user = %+v, want %+v", user, want)
	}
}

// The signature covers a sorted key=value string, so the order in the wire
// format is irrelevant. Telegram does not promise an order either.
func TestValidateInitDataAcceptsReorderedFields(t *testing.T) {
	now := time.Unix(testNowUnix, 0)
	fields := baseFields(testNowUnix)
	raw, hash := signInitData(testBotToken, fields)

	reordered := []initDataField{fields[2], fields[0], fields[1]}
	reorderedRaw := encodeInitData(reordered, hash)

	for name, candidate := range map[string]string{"declared order": raw, "reordered": reorderedRaw} {
		t.Run(name, func(t *testing.T) {
			user, err := ValidateInitData(testBotToken, candidate, now, time.Hour)
			if err != nil {
				t.Fatalf("ValidateInitData: %v", err)
			}
			if user.ID != testUserID {
				t.Fatalf("user id = %d, want %d", user.ID, testUserID)
			}
		})
	}
}

// The fixed vector proves that the production signing string and the published
// algorithm agree, including URL decoding of the user JSON.
func TestValidateInitDataMatchesFixedKnownAnswer(t *testing.T) {
	if got := independentInitDataHash(testBotToken, fixedFields()); got != fixedInitDataHash {
		t.Fatalf("independent signer produced %q, want known answer %q", got, fixedInitDataHash)
	}

	values, err := url.ParseQuery(fixedInitDataRaw)
	if err != nil {
		t.Fatalf("parse fixed initData: %v", err)
	}
	if got := values.Get("hash"); got != fixedInitDataHash {
		t.Fatalf("fixed raw hash = %q, want %q", got, fixedInitDataHash)
	}
	// Assert the canonical string the implementation signs, independently of
	// how it is hashed: key order, "=" joining and "\n" separators are all
	// exactly what Telegram documents.
	if got := initDataCheckString(values); got != fixedCheckString {
		t.Fatalf("data_check_string = %q, want %q", got, fixedCheckString)
	}

	user, err := ValidateInitData(testBotToken, fixedInitDataRaw, time.Unix(testNowUnix, 0), time.Hour)
	if err != nil {
		t.Fatalf("ValidateInitData on known-answer vector: %v", err)
	}
	if user.ID != testUserID || user.Username != "tester" || user.FirstName != "Test" {
		t.Fatalf("user = %+v, want id %d, username tester, first name Test", user, testUserID)
	}

	forged := strings.Replace(fixedInitDataRaw, fixedInitDataHash, flipLastHexDigit(fixedInitDataHash), 1)
	if _, err := ValidateInitData(testBotToken, forged, time.Unix(testNowUnix, 0), time.Hour); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("forged known-answer vector: error = %v, want ErrUnauthorized", err)
	}
}

func TestValidateInitDataRejectsTampering(t *testing.T) {
	now := time.Unix(testNowUnix, 0)
	fields := baseFields(testNowUnix)
	validRaw, validHash := signInitData(testBotToken, fields)

	t.Run("signature and encoding", func(t *testing.T) {
		cases := []struct {
			name  string
			raw   string
			token string
		}{
			{
				name:  "username changed after signing",
				raw:   strings.Replace(validRaw, "tester", "attacker", 1),
				token: testBotToken,
			},
			{
				name:  "user id changed after signing",
				raw:   strings.Replace(validRaw, "%3A10001%2C", "%3A10002%2C", 1),
				token: testBotToken,
			},
			{
				name:  "one signature nibble changed",
				raw:   encodeInitData(fields, flipLastHexDigit(validHash)),
				token: testBotToken,
			},
			{
				name:  "hash missing",
				raw:   strings.TrimSuffix(validRaw, "&hash="+validHash),
				token: testBotToken,
			},
			{
				name:  "hash too short",
				raw:   encodeInitData(fields, validHash[:63]),
				token: testBotToken,
			},
			{
				name:  "hash not hexadecimal",
				raw:   encodeInitData(fields, strings.Repeat("z", 64)),
				token: testBotToken,
			},
			{
				name:  "hash repeated",
				raw:   validRaw + "&hash=" + validHash,
				token: testBotToken,
			},
			{
				name:  "broken percent escape",
				raw:   validRaw + "&user=%zz",
				token: testBotToken,
			},
			{
				name:  "signed by another bot",
				raw:   validRaw,
				token: "10002:TEST-TOKEN",
			},
			{
				name:  "empty bot token fails closed",
				raw:   validRaw,
				token: "",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				user, err := ValidateInitData(tc.token, tc.raw, now, time.Hour)
				if !errors.Is(err, ErrUnauthorized) {
					t.Fatalf("error = %v, want ErrUnauthorized", err)
				}
				if user != (InitDataUser{}) {
					t.Fatalf("user = %+v on failure, want zero value", user)
				}
			})
		}
	})

	// These payloads are signed correctly but describe an unusable identity,
	// so they must be rejected even though the HMAC matches.
	t.Run("signed but malformed payload", func(t *testing.T) {
		cases := []struct {
			name   string
			fields []initDataField
		}{
			{"auth_date missing", withoutField(fields, "auth_date")},
			{"auth_date not numeric", withField(fields, "auth_date", "yesterday")},
			{"user missing", withoutField(fields, "user")},
			{"user not JSON", withField(fields, "user", "not-json")},
			{"user id not positive", withField(fields, "user", testUserJSON(0, "tester"))},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				raw, _ := signInitData(testBotToken, tc.fields)
				if _, err := ValidateInitData(testBotToken, raw, now, time.Hour); !errors.Is(err, ErrUnauthorized) {
					t.Fatalf("error = %v, want ErrUnauthorized", err)
				}
			})
		}
	})
}

func TestValidateInitDataEnforcesFreshness(t *testing.T) {
	now := time.Unix(testNowUnix, 0)

	cases := []struct {
		name     string
		authUnix int64
		ttl      time.Duration
		want     error
	}{
		{"well within ttl", testNowUnix - 30*60, time.Hour, nil},
		{"exactly ttl old is still fresh", testNowUnix - 3600, time.Hour, nil},
		{"older than ttl", testNowUnix - 3601, time.Hour, ErrExpired},
		{"future but within clock skew", testNowUnix + 30, time.Hour, nil},
		{"future at clock skew boundary", testNowUnix + 60, time.Hour, nil},
		{"future beyond clock skew", testNowUnix + 61, time.Hour, ErrUnauthorized},
		{"zero ttl falls back to one hour", testNowUnix - 30*60, 0, nil},
		{"negative ttl falls back to one hour", testNowUnix - 30*60, -time.Hour, nil},
		{"default ttl still expires", testNowUnix - 61*60, 0, ErrExpired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := signInitData(testBotToken, baseFields(tc.authUnix))
			_, err := ValidateInitData(testBotToken, raw, now, tc.ttl)

			switch {
			case tc.want == nil && err != nil:
				t.Fatalf("error = %v, want acceptance", err)
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want == ErrExpired && errors.Is(err, ErrUnauthorized) {
				t.Fatalf("expired data reported as ErrUnauthorized")
			}
		})
	}
}

// signedRequest builds a request carrying raw initData in the header the Mini
// App uses.
func signedRequest(raw string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/statistics", nil)
	if raw != "" {
		req.Header.Set("X-Telegram-Init-Data", raw)
	}
	return req
}

func TestInitDataMiddleware(t *testing.T) {
	nowUnix := time.Now().Unix()
	validRaw, _ := signInitData(testBotToken, baseFields(nowUnix))
	expiredRaw, _ := signInitData(testBotToken, baseFields(time.Now().Add(-2*time.Hour).Unix()))
	tamperedRaw := strings.Replace(validRaw, "tester", "attacker", 1)

	const unauthorizedBody = `{"error":{"code":"unauthorized"}}`

	cases := []struct {
		name       string
		raw        string
		wantStatus int
		wantNext   bool
		wantUser   bool
	}{
		{"missing header", "", http.StatusUnauthorized, false, false},
		{"valid header", validRaw, http.StatusNoContent, true, true},
		{"tampered header", tamperedRaw, http.StatusUnauthorized, false, false},
		{"expired header", expiredRaw, http.StatusUnauthorized, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				nextCalled bool
				gotUser    InitDataUser
				gotOK      bool
			)

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				gotUser, gotOK = UserFromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})
			handler := NewInitDataMiddleware(testBotToken, time.Hour)(next)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, signedRequest(tc.raw))

			if nextCalled != tc.wantNext {
				t.Fatalf("next called = %v, want %v", nextCalled, tc.wantNext)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if !tc.wantNext {
				if body := rec.Body.String(); body != unauthorizedBody {
					t.Fatalf("body = %q, want %q", body, unauthorizedBody)
				}
				if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
					t.Fatalf("Content-Type = %q, want application/json", contentType)
				}
				return
			}

			if !gotOK {
				t.Fatal("UserFromContext reported no user for an authenticated request")
			}
			if gotUser.ID != testUserID {
				t.Fatalf("user id = %d, want %d", gotUser.ID, testUserID)
			}
			if gotUser.Username != "tester" {
				t.Fatalf("username = %q, want tester", gotUser.Username)
			}
		})
	}
}

// One middleware instance must not retain the identity of an earlier request:
// the context value is per request and never stored on the handler.
func TestInitDataMiddlewareKeepsIdentityPerRequest(t *testing.T) {
	nowUnix := time.Now().Unix()
	firstRaw, _ := signInitData(testBotToken, baseFields(nowUnix))
	secondFields := withField(baseFields(nowUnix), "user", testUserJSON(20002, "other"))
	secondRaw, _ := signInitData(testBotToken, secondFields)

	handler := NewInitDataMiddleware(testBotToken, time.Hour)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			http.Error(w, "missing user", http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, strconv.FormatInt(user.ID, 10))
	}))

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"first", firstRaw, "10001"},
		{"second", secondRaw, "20002"},
		{"first again", firstRaw, "10001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, signedRequest(tc.raw))
			if got := rec.Body.String(); got != tc.want {
				t.Fatalf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

// A middleware constructed without a token must reject everyone rather than
// fall back to a signature over an empty, publicly known key.
func TestInitDataMiddlewareFailsClosedWithoutToken(t *testing.T) {
	raw, _ := signInitData(testBotToken, baseFields(time.Now().Unix()))
	handler := NewInitDataMiddleware("", time.Hour)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request with an unset bot token reached the next handler")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedRequest(raw))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// Authentication failures are logged, but the log must stay safe to ship to a
// less protected sink: no initData, no signature, no bot token.
func TestInitDataMiddlewareLogsWithoutCredential(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	raw, hash := signInitData(testBotToken, baseFields(time.Now().Unix()))
	forged := strings.Replace(raw, "tester", "attacker", 1)

	handler := NewInitDataMiddleware(testBotToken, time.Hour)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("rejected request reached the next handler")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedRequest(forged))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	line := buf.String()
	for _, want := range []string{"mini app init data rejected", "/api/statistics", "signature or payload invalid"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %q is missing %q", line, want)
		}
	}
	for _, secret := range []string{forged, hash, testBotToken, "auth_date=", "WebAppData"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log line leaked credential material %q: %q", secret, line)
		}
	}
}

func TestUserFromContextWithoutMiddleware(t *testing.T) {
	if user, ok := UserFromContext(context.Background()); ok || user != (InitDataUser{}) {
		t.Fatalf("UserFromContext(background) = %+v, %v; want zero, false", user, ok)
	}

	var nilContext context.Context
	if user, ok := UserFromContext(nilContext); ok || user != (InitDataUser{}) {
		t.Fatalf("UserFromContext(nil) = %+v, %v; want zero, false", user, ok)
	}
}

func TestWriteJSONAndWriteError(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteJSON(rec, http.StatusCreated, struct {
			ID int64 `json:"id"`
		}{ID: testUserID})

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
		}
		if body := rec.Body.String(); body != `{"id":10001}` {
			t.Fatalf("body = %q, want %q", body, `{"id":10001}`)
		}
		if contentType := rec.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
			t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", contentType)
		}
	})

	t.Run("error envelope", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteError(rec, http.StatusUnauthorized, "unauthorized")

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		if body := rec.Body.String(); body != `{"error":{"code":"unauthorized"}}` {
			t.Fatalf("body = %q, want %q", body, `{"error":{"code":"unauthorized"}}`)
		}
	})

	t.Run("unencodable value", func(t *testing.T) {
		previous := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		t.Cleanup(func() { slog.SetDefault(previous) })

		rec := httptest.NewRecorder()
		WriteJSON(rec, http.StatusOK, make(chan int))

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
		if body := rec.Body.String(); body != `{"error":{"code":"internal_error"}}` {
			t.Fatalf("body = %q, want %q", body, `{"error":{"code":"internal_error"}}`)
		}
	})
}
