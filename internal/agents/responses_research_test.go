package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIResponsesResearchClientParsesEvidenceAndSources(t *testing.T) {
	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_123",
			"model": "gpt-5.4-mini",
			"output": [
				{
					"type": "web_search_call",
					"action": {
						"sources": [
							{"title": "Exchange filing", "url": "https://example.com/filing"}
						]
					}
				},
				{
					"type": "message",
					"content": [
						{
							"type": "output_text",
							"text": "{\"summary\":\"Fresh order win supports sentiment.\",\"sentiment\":\"positive\",\"sentiment_score\":0.4,\"confidence\":72,\"catalysts\":[{\"text\":\"Order win announced\",\"impact\":\"positive\",\"time_horizon\":\"short-term\",\"sources\":[\"https://example.com/filing\"]}]}",
							"annotations": [
								{"type": "url_citation", "title": "News story", "url": "https://example.com/news"}
							]
						}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewOpenAIResponsesResearchClient("test-key", "gpt-5.4-mini", "low")
	client.baseURL = server.URL
	client.httpClient = server.Client()

	report, err := client.ResearchSymbol(context.Background(), ResearchEvidenceRequest{
		Symbol:         "RELIANCE",
		CompanyName:    "Reliance Industries Ltd",
		AllowedDomains: []string{"example.com"},
		LiveWebAccess:  true,
	})
	if err != nil {
		t.Fatalf("research symbol: %v", err)
	}
	if report.Symbol != "RELIANCE" || report.Sentiment != "positive" || report.Confidence != 72 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(report.Catalysts) != 1 || report.Catalysts[0].Sources[0] != "https://example.com/filing" {
		t.Fatalf("unexpected catalysts: %#v", report.Catalysts)
	}
	if len(report.Sources) != 2 {
		t.Fatalf("expected merged sources, got %#v", report.Sources)
	}

	if gotPayload["model"] != "gpt-5.4-mini" || gotPayload["tool_choice"] != "auto" {
		t.Fatalf("unexpected payload: %#v", gotPayload)
	}
	tools := gotPayload["tools"].([]interface{})
	tool := tools[0].(map[string]interface{})
	if tool["type"] != "web_search" || tool["external_web_access"] != true {
		t.Fatalf("unexpected web search tool: %#v", tool)
	}
	filters := tool["filters"].(map[string]interface{})
	domains := filters["allowed_domains"].([]interface{})
	if domains[0] != "example.com" {
		t.Fatalf("unexpected domain filter: %#v", domains)
	}
}

func TestParseResearchEvidenceTextRejectsNonJSON(t *testing.T) {
	if _, err := parseResearchEvidenceText("not json"); err == nil {
		t.Fatal("expected non-json research text to fail")
	}
}

func TestParseResearchEvidenceTextNormalizesFractionalConfidence(t *testing.T) {
	report, err := parseResearchEvidenceText(`{"summary":"ok","sentiment":"mixed","sentiment_score":0.2,"confidence":0.82}`)
	if err != nil {
		t.Fatalf("parse evidence: %v", err)
	}
	if report.Confidence != 82 {
		t.Fatalf("expected confidence 82, got %.2f", report.Confidence)
	}
}
