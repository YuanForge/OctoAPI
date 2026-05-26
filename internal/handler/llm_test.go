package handler

import (
	"testing"

	"fanapi/internal/protocol"
)

func TestShouldConvertRequestBodyResponsesToResponsesWithMessages(t *testing.T) {
	reqData := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}

	if !shouldConvertRequestBody(protocolResponses, protocolResponses, reqData) {
		t.Fatal("expected conversion for responses->responses when top-level messages is non-empty")
	}
}

func TestShouldConvertRequestBodyResponsesToResponsesNativeInput(t *testing.T) {
	reqData := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "hello"},
				},
			},
		},
	}

	if shouldConvertRequestBody(protocolResponses, protocolResponses, reqData) {
		t.Fatal("expected no conversion for native responses input without top-level messages")
	}
}

func TestShouldConvertRequestBodyResponsesNativeAssistantOutputTextPreserved(t *testing.T) {
	reqData := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "output_text", "text": "你好"},
				},
			},
		},
	}

	if shouldConvertRequestBody(protocolResponses, protocolResponses, reqData) {
		t.Fatal("expected no conversion for native responses input")
	}

	input, _ := reqData["input"].([]interface{})
	item, _ := input[0].(map[string]interface{})
	content, _ := item["content"].([]interface{})
	part, _ := content[0].(map[string]interface{})
	if part["type"] != "output_text" {
		t.Fatalf("expected assistant output_text part preserved, got %#v", part["type"])
	}

	normalized, err := protocol.NormalizeClientRequest(reqData, protocolResponses)
	if err != nil {
		t.Fatalf("unexpected normalize error: %v", err)
	}
	roundTripped, err := protocol.ConvertRequest(normalized, protocolResponses)
	if err != nil {
		t.Fatalf("unexpected convert error: %v", err)
	}
	rtInput, _ := roundTripped["input"].([]interface{})
	rtItem, _ := rtInput[0].(map[string]interface{})
	if _, isString := rtItem["content"].(string); !isString {
		t.Fatalf("expected current round-trip to alter assistant content shape for regression context, got %#v", rtItem["content"])
	}
}
