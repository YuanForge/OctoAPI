package service

import (
	"regexp"
	"strings"
)

const genericUpstreamErrorMessage = "上游服务暂时不可用，请稍后重试"

var urlRegexp = regexp.MustCompile(`https?://\S+`)

// UserFacingErrorMessage removes internal upstream details such as URLs, IPs,
// and network dial errors before returning errors to users.
// Internal prefixes are stripped so that the real upstream error content is preserved.
func UserFacingErrorMessage(msg string) string {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return "请求失败，请稍后重试"
	}
	lower := strings.ToLower(trimmed)

	// Strip internal prefixes and recursively process the remainder.
	for _, prefix := range []string{
		"upstream error: ",
		"request mapping error: ",
		"response mapping error: ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return UserFacingErrorMessage(trimmed[len(prefix):])
		}
	}

	// Pure network / infrastructure errors → generic message.
	for _, marker := range []string{
		"dial tcp",
		"i/o timeout",
		"client.timeout",
		"context deadline exceeded",
		"no such host",
		"connection refused",
		"connection reset",
		"malformed http response",
		"transport connection broken",
		"tls handshake",
		"retry publish failed",
		"unexpected eof",
	} {
		if strings.Contains(lower, marker) {
			return genericUpstreamErrorMessage
		}
	}

	// Strip URLs from the message rather than hiding the whole error.
	result := strings.TrimSpace(urlRegexp.ReplaceAllString(trimmed, ""))
	if result == "" {
		return genericUpstreamErrorMessage
	}
	return result
}
