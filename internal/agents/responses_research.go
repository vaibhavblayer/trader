package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const defaultResponsesBaseURL = "https://api.openai.com/v1"

// ResearchEvidenceClient fetches cited, current research evidence for a symbol.
type ResearchEvidenceClient interface {
	ResearchSymbol(ctx context.Context, req ResearchEvidenceRequest) (*ResearchEvidenceReport, error)
}

// OpenAIResponsesResearchClient uses the OpenAI Responses API web_search tool.
type OpenAIResponsesResearchClient struct {
	apiKey          string
	model           string
	reasoningEffort string
	baseURL         string
	httpClient      *http.Client
}

// NewOpenAIResponsesResearchClient creates an OpenAI Responses client for web research.
func NewOpenAIResponsesResearchClient(apiKey, model, reasoningEffort string) *OpenAIResponsesResearchClient {
	return &OpenAIResponsesResearchClient{
		apiKey:          apiKey,
		model:           model,
		reasoningEffort: reasoningEffort,
		baseURL:         defaultResponsesBaseURL,
		httpClient:      &http.Client{Timeout: 45 * time.Second},
	}
}

// ResearchEvidenceRequest describes a symbol research query.
type ResearchEvidenceRequest struct {
	Symbol         string
	CompanyName    string
	Sector         string
	Industry       string
	Exchange       string
	CurrentPrice   float64
	AllowedDomains []string
	LiveWebAccess  bool
}

// ResearchEvidenceReport is a cited, advisory-only research report.
type ResearchEvidenceReport struct {
	Symbol         string                   `json:"symbol"`
	Model          string                   `json:"model,omitempty"`
	GeneratedAt    time.Time                `json:"generated_at"`
	Summary        string                   `json:"summary"`
	Sentiment      string                   `json:"sentiment"`
	SentimentScore float64                  `json:"sentiment_score"`
	Confidence     float64                  `json:"confidence"`
	Catalysts      []ResearchEvidencePoint  `json:"catalysts,omitempty"`
	Risks          []ResearchEvidencePoint  `json:"risks,omitempty"`
	EventRisks     []ResearchEvidencePoint  `json:"event_risks,omitempty"`
	SectorContext  []ResearchEvidencePoint  `json:"sector_context,omitempty"`
	Sources        []ResearchEvidenceSource `json:"sources,omitempty"`
}

// ResearchEvidencePoint is one cited evidence point.
type ResearchEvidencePoint struct {
	Text        string   `json:"text"`
	Impact      string   `json:"impact,omitempty"`
	TimeHorizon string   `json:"time_horizon,omitempty"`
	Sources     []string `json:"sources,omitempty"`
}

// ResearchEvidenceSource is a source consulted or cited by the model.
type ResearchEvidenceSource struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
}

// ResearchSymbol performs a Responses API request with the web_search tool.
func (c *OpenAIResponsesResearchClient) ResearchSymbol(ctx context.Context, req ResearchEvidenceRequest) (*ResearchEvidenceReport, error) {
	if c == nil || strings.TrimSpace(c.apiKey) == "" {
		return nil, fmt.Errorf("openai api key not configured")
	}
	model := strings.TrimSpace(c.model)
	if model == "" {
		model = "gpt-5.4-mini"
	}

	payload := c.researchPayload(model, req)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.baseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai responses request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai responses returned %s: %s", resp.Status, truncateForError(string(respBody), 500))
	}

	var parsed responsesAPIResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	text := parsed.Text()
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("openai response did not include message text")
	}

	report, err := parseResearchEvidenceText(text)
	if err != nil {
		return nil, err
	}
	report.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	report.Model = parsed.Model
	if report.Model == "" {
		report.Model = model
	}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = time.Now()
	}
	report.Sources = mergeEvidenceSources(report.Sources, parsed.Sources())
	return report, nil
}

func (c *OpenAIResponsesResearchClient) researchPayload(model string, req ResearchEvidenceRequest) map[string]interface{} {
	tool := map[string]interface{}{
		"type":                "web_search",
		"external_web_access": req.LiveWebAccess,
		"user_location": map[string]interface{}{
			"type":     "approximate",
			"country":  "IN",
			"timezone": "Asia/Kolkata",
		},
	}
	if len(req.AllowedDomains) > 0 {
		tool["filters"] = map[string]interface{}{"allowed_domains": req.AllowedDomains}
	}

	payload := map[string]interface{}{
		"model":        model,
		"instructions": researchEvidenceInstructions(),
		"input":        researchEvidencePrompt(req),
		"tools":        []map[string]interface{}{tool},
		"tool_choice":  "auto",
		"include":      []string{"web_search_call.action.sources"},
		"store":        false,
	}
	if c.reasoningEffort != "" {
		payload["reasoning"] = map[string]string{"effort": c.reasoningEffort}
	}
	return payload
}

func researchEvidenceInstructions() string {
	return `You are a market research evidence analyst for Indian equities.
Use web search for current public information. You must not recommend or place trades.
Return only valid JSON with this schema:
{
  "summary": "one concise paragraph",
  "sentiment": "positive|neutral|negative|mixed",
  "sentiment_score": -1.0,
  "confidence": 0,
  "catalysts": [{"text":"...", "impact":"positive|negative|neutral", "time_horizon":"intraday|short-term|medium-term", "sources":["https://..."]}],
  "risks": [{"text":"...", "impact":"positive|negative|neutral", "time_horizon":"intraday|short-term|medium-term", "sources":["https://..."]}],
  "event_risks": [{"text":"...", "impact":"positive|negative|neutral", "time_horizon":"intraday|short-term|medium-term", "sources":["https://..."]}],
  "sector_context": [{"text":"...", "impact":"positive|negative|neutral", "time_horizon":"intraday|short-term|medium-term", "sources":["https://..."]}]
}
Every non-empty evidence point must include at least one source URL. If evidence is weak, say so and lower confidence.`
}

func researchEvidencePrompt(req ResearchEvidenceRequest) string {
	var sb strings.Builder
	sb.WriteString("Research current public evidence for this Indian equity candidate.\n")
	sb.WriteString(fmt.Sprintf("Symbol: %s\n", strings.ToUpper(strings.TrimSpace(req.Symbol))))
	if req.Exchange != "" {
		sb.WriteString(fmt.Sprintf("Exchange: %s\n", req.Exchange))
	}
	if req.CompanyName != "" {
		sb.WriteString(fmt.Sprintf("Company: %s\n", req.CompanyName))
	}
	if req.Sector != "" {
		sb.WriteString(fmt.Sprintf("Sector: %s\n", req.Sector))
	}
	if req.Industry != "" {
		sb.WriteString(fmt.Sprintf("Industry: %s\n", req.Industry))
	}
	if req.CurrentPrice > 0 {
		sb.WriteString(fmt.Sprintf("Current price: %.2f INR\n", req.CurrentPrice))
	}
	sb.WriteString("\nFocus on: latest news, earnings/results, regulatory or corporate events, sector context, and intraday/short-term event risk. Avoid uncited claims.")
	return sb.String()
}

type responsesAPIResponse struct {
	ID         string                `json:"id"`
	Model      string                `json:"model"`
	OutputText string                `json:"output_text"`
	Output     []responsesOutputItem `json:"output"`
}

type responsesOutputItem struct {
	Type    string                   `json:"type"`
	Content []responsesContentItem   `json:"content"`
	Action  responsesWebSearchAction `json:"action"`
}

type responsesContentItem struct {
	Type        string                        `json:"type"`
	Text        string                        `json:"text"`
	Annotations []responsesCitationAnnotation `json:"annotations"`
}

type responsesCitationAnnotation struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type responsesWebSearchAction struct {
	Sources []responsesCitationAnnotation `json:"sources"`
}

func (r responsesAPIResponse) Text() string {
	if strings.TrimSpace(r.OutputText) != "" {
		return r.OutputText
	}
	var parts []string
	for _, item := range r.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (r responsesAPIResponse) Sources() []ResearchEvidenceSource {
	var sources []ResearchEvidenceSource
	for _, item := range r.Output {
		if item.Type == "web_search_call" {
			for _, source := range item.Action.Sources {
				sources = append(sources, ResearchEvidenceSource{Title: source.Title, URL: source.URL})
			}
		}
		for _, content := range item.Content {
			for _, annotation := range content.Annotations {
				if annotation.URL != "" {
					sources = append(sources, ResearchEvidenceSource{Title: annotation.Title, URL: annotation.URL})
				}
			}
		}
	}
	return normalizeEvidenceSources(sources)
}

func parseResearchEvidenceText(text string) (*ResearchEvidenceReport, error) {
	raw := extractJSONObject(text)
	if raw == "" {
		return nil, fmt.Errorf("openai research response was not JSON")
	}
	var report ResearchEvidenceReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, fmt.Errorf("parse openai research JSON: %w", err)
	}
	report.Summary = strings.TrimSpace(report.Summary)
	report.Sentiment = strings.ToLower(strings.TrimSpace(report.Sentiment))
	report.Confidence = normalizeEvidenceConfidence(report.Confidence)
	report.SentimentScore = clampFloat(report.SentimentScore, -1, 1)
	return &report, nil
}

func normalizeEvidenceConfidence(value float64) float64 {
	if value > 0 && value <= 1 {
		value *= 100
	}
	return clampFloat(value, 0, 100)
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return ""
	}
	return text[start : end+1]
}

func mergeEvidenceSources(a, b []ResearchEvidenceSource) []ResearchEvidenceSource {
	return normalizeEvidenceSources(append(a, b...))
}

func normalizeEvidenceSources(sources []ResearchEvidenceSource) []ResearchEvidenceSource {
	seen := make(map[string]ResearchEvidenceSource)
	for _, source := range sources {
		url := strings.TrimSpace(source.URL)
		if url == "" {
			continue
		}
		existing := seen[url]
		if existing.Title == "" {
			existing.Title = strings.TrimSpace(source.Title)
		}
		existing.URL = url
		seen[url] = existing
	}
	out := make([]ResearchEvidenceSource, 0, len(seen))
	for _, source := range seen {
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func truncateForError(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
