package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"html"
	"regexp"
	"strings"
)

var (
	commentRegex     = regexp.MustCompile(`<!--[\s\S]*?-->`)
	styleTagRegex    = regexp.MustCompile(`<style[^>]*>[\s\S]*?</style>`)
	inlineStyleRegex = regexp.MustCompile(`<[^>]*style="[^"]*"[^>]*>`)
	h1OpenRegex      = regexp.MustCompile(`<h1[^>]*>`)
	h1CloseRegex     = regexp.MustCompile(`</h1>`)
	brRegex          = regexp.MustCompile(`<br\s*/>`)
	htmlTagRegex     = regexp.MustCompile(`<[^>]*>`)
	// Add whitespace replacer as a package-level variable
	whitespaceReplacer = strings.NewReplacer(
		`\r\n`, "",
		"\r\n", "",
		`\t`, "",
		`\f`, "",
	)

	// Add spaceRegex as a package-level variable
	spaceRegex = regexp.MustCompile(`\s+`)
)

const publicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCS2TZDs5+orLYCL5SsJ54+bPCV
s1ZQQwP2RoPkFQF2jcT0HnNNT8ZoQgJTrGwNi5QNTBDoHC4oJesAVYe6DoxXS9Nl
s8WbGE8ZNgOC5tVv1WVjyBw7k2x72C/qjPoyo/kO7TYl6Qnu4jqW/ImLoup/nsJp
pUznF0YgbyU/dFFNBQIDAQAB
-----END PUBLIC KEY-----`

func CleanContent(htmlContent string) string {
	content := html.UnescapeString(htmlContent)
	// 1. Replace \" with "
	// content = strings.ReplaceAll(content, `\"`, `"`)
	// 2. Remove HTML comments with their content
	content = commentRegex.ReplaceAllString(content, "")
	// 3. Handle escaped line breaks and whitespace
	content = whitespaceReplacer.Replace(content)
	// Handle any remaining whitespace
	content = spaceRegex.ReplaceAllString(content, " ")
	// 4. Remove all HTML tags:
	// Remove <style> tags and their content
	content = styleTagRegex.ReplaceAllString(content, "")
	// Remove tags with inline styles
	content = inlineStyleRegex.ReplaceAllString(content, "")

	// 5. Handle block elements (<div> and <p>) with line breaks
	// First replace existing <br /> patterns to avoid duplication
	content = strings.ReplaceAll(content, "</div><br />", "||DIV_BR||")
	content = strings.ReplaceAll(content, "</p><br />", "||P_BR||")
	// Add <br /> after block elements
	content = strings.ReplaceAll(content, "</div>", "</div><br />")
	content = strings.ReplaceAll(content, "</p>", "</p><br />")
	// Restore original patterns
	content = strings.ReplaceAll(content, "||DIV_BR||", "</div><br />")
	content = strings.ReplaceAll(content, "||P_BR||", "</p><br />")

	// 6. Replace <h1> tags with <br />
	content = h1OpenRegex.ReplaceAllString(content, "<br />")
	content = h1CloseRegex.ReplaceAllString(content, "")

	// 7. Replace <br /> tags with newline
	content = brRegex.ReplaceAllString(content, "\n")

	// 6. Remove all remaining HTML tags
	content = htmlTagRegex.ReplaceAllString(content, "")

	// 9. Trim any remaining whitespace
	content = strings.TrimSpace(content)

	return content
}

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
