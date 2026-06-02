package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/swaggo/swag"
)

func TestIsHTTPSRequestHonorsForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodGet, "http://ai.midaccs.com/openapi-user.json", nil)
	req.Header.Set("X-Forwarded-Proto", "https, http")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if !isHTTPSRequest(c, "ai.midaccs.com") {
		t.Fatal("expected forwarded https request to use https scheme")
	}
}

func TestIsHTTPSRequestInfersPublicHostAsHTTPS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodGet, "http://ai.midaccs.com/openapi-user.json", nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if !isHTTPSRequest(c, "ai.midaccs.com") {
		t.Fatal("expected public host docs to default to https scheme")
	}
}

func TestIsHTTPSRequestKeepsLocalhostHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/openapi-user.json", nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if isHTTPSRequest(c, "localhost:8080") {
		t.Fatal("expected localhost docs to keep http scheme")
	}
}

func TestBuildUserSwaggerDocIncludesLLMSchemas(t *testing.T) {
	doc, err := swag.ReadDoc()
	if err != nil {
		t.Fatalf("read swagger doc: %v", err)
	}
	filtered, err := buildUserSwaggerDoc([]byte(doc))
	if err != nil {
		t.Fatalf("build user swagger doc: %v", err)
	}

	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Schema map[string]string `json:"schema"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(filtered, &spec); err != nil {
		t.Fatalf("unmarshal filtered doc: %v", err)
	}

	assertBodyRef := func(path, method, want string) {
		t.Helper()
		op, ok := spec.Paths[path][method]
		if !ok {
			t.Fatalf("missing operation %s %s", method, path)
		}
		for _, param := range op.Parameters {
			if param.Schema["$ref"] == want {
				return
			}
		}
		t.Fatalf("operation %s %s missing body schema ref %s", method, path, want)
	}

	assertBodyRef("/v1/chat/completions", "post", "#/definitions/model.OpenAIChatCompletionRequest")
	assertBodyRef("/v1/messages", "post", "#/definitions/model.ClaudeMessagesRequest")
	assertBodyRef("/v1/gemini", "post", "#/definitions/model.GeminiGenerateContentRequest")
	assertBodyRef("/v1/responses", "post", "#/definitions/model.ResponsesRequest")
	assertBodyRef("/v1/responses/compact", "post", "#/definitions/model.ResponsesRequest")
	assertBodyRef("/v1beta/models/{path}", "post", "#/definitions/model.GeminiGenerateContentRequest")
}
