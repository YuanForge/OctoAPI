package service

import "testing"

func TestUserFacingErrorMessageHidesUpstreamNetworkDetails(t *testing.T) {
	msg := `upstream error: Post "https://cpa.fanapi.cc/v1/images/generations": dial tcp 64.83.32.155:443: i/o timeout`
	if got := UserFacingErrorMessage(msg); got != genericUpstreamErrorMessage {
		t.Fatalf("expected generic upstream message, got %q", got)
	}
}

func TestUserFacingErrorMessageHidesConnectionResetDetails(t *testing.T) {
	msg := `upstream error: Post "https://api.apimart.ai/v1/images/generations": read tcp 172.19.0.2:42354->104.26.11.94:443: read: connection reset by peer`
	if got := UserFacingErrorMessage(msg); got != genericUpstreamErrorMessage {
		t.Fatalf("expected generic upstream message, got %q", got)
	}
}

func TestUserFacingErrorMessageHidesMalformedHTTPResponseDetails(t *testing.T) {
	msg := `upstream error: Post "https://api.chatfire.cn/v1/images/generations": net/http: HTTP/1.x transport connection broken: malformed HTTP response "\x00\x00\x12\x04"`
	if got := UserFacingErrorMessage(msg); got != genericUpstreamErrorMessage {
		t.Fatalf("expected generic upstream message, got %q", got)
	}
}

func TestUserFacingErrorMessageKeepsBusinessMessage(t *testing.T) {
	msg := "余额不足"
	if got := UserFacingErrorMessage(msg); got != msg {
		t.Fatalf("expected business message to pass through, got %q", got)
	}
}

func TestUserFacingErrorMessageStripsUpstreamErrorPrefix(t *testing.T) {
	msg := `upstream error: upstream returned 400: {"error":{"message":"Invalid API key"}}`
	got := UserFacingErrorMessage(msg)
	if got == genericUpstreamErrorMessage {
		t.Fatalf("expected real error content, got generic message")
	}
	want := `upstream returned 400: {"error":{"message":"Invalid API key"}}`
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestUserFacingErrorMessageStripsURL(t *testing.T) {
	msg := "upstream returned 403: access denied to https://api.example.com/v1/chat"
	got := UserFacingErrorMessage(msg)
	if got == genericUpstreamErrorMessage {
		t.Fatalf("expected message with URL stripped, got generic message")
	}
	if got == msg {
		t.Fatalf("expected URL to be stripped, got original message")
	}
}
