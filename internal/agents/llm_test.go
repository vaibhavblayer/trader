package agents

import (
	"testing"

	"github.com/sashabaranov/go-openai"
)

func TestApplyJSONResponseFormatOnlyWhenPromptRequestsJSON(t *testing.T) {
	req := openai.ChatCompletionRequest{}
	applyJSONResponseFormat(&req, "Respond ONLY with valid JSON.")
	if req.ResponseFormat == nil {
		t.Fatal("expected JSON response format")
	}
	if req.ResponseFormat.Type != openai.ChatCompletionResponseFormatTypeJSONObject {
		t.Fatalf("expected json_object response format, got %s", req.ResponseFormat.Type)
	}

	req = openai.ChatCompletionRequest{}
	applyJSONResponseFormat(&req, "Provide a concise paragraph.")
	if req.ResponseFormat != nil {
		t.Fatalf("did not expect response format, got %#v", req.ResponseFormat)
	}
}
