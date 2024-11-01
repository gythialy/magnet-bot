package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
)

const publicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCS2TZDs5+orLYCL5SsJ54+bPCV
s1ZQQwP2RoPkFQF2jcT0HnNNT8ZoQgJTrGwNi5QNTBDoHC4oJesAVYe6DoxXS9Nl
s8WbGE8ZNgOC5tVv1WVjyBw7k2x72C/qjPoyo/kO7TYl6Qnu4jqW/ImLoup/nsJp
pUznF0YgbyU/dFFNBQIDAQAB
-----END PUBLIC KEY-----`

func ToString(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return ""
	}
	return string(b)
}

// EscapeMarkdown
// In all other places characters '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'
// must be escaped with the preceding character '\'.
// In all other places characters '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!'
// must be escaped with the preceding character '\'.
func EscapeMarkdown(s string) string {
	return strings.NewReplacer(
		"*", "",
		"#", "",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"（", "\\(",
		"）", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	).Replace(s)
}

// headerValue := fmt.Sprintf("%s%s$$%d", baseURL, endpoint, time.Now().UnixMilli())
// encryptedHeader, err := encryptByRSA(headerValue)
// if err != nil {
// return fmt.Errorf("encryption error: %w", err)
// }
//
// // Create new request
// url := baseURL + endpoint + data
// req, err := http.NewRequest("GET", url, nil)
// if err != nil {
// return fmt.Errorf("failed to create request: %w", err)
// }
//
// // Set headers
// req.Header.Set("nsssjss", encryptedHeader)
// req.Header.Set("Content-Type", "application/json; charset=utf-8")
func EncryptByRSA(value string) (string, error) {
	// The public key in PEM format

	// Decode PEM block
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return "", errors.New("failed to parse PEM block containing the public key")
	}

	// Parse public key
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}

	// Convert to RSA public key
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("not an RSA public key")
	}

	// Encrypt the data
	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte(value))
	if err != nil {
		return "", err
	}

	// Encode to base64
	return base64.StdEncoding.EncodeToString(encrypted), nil
}
