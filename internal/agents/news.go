// Package agents provides AI agent implementations for trading decisions.
package agents

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// NewsAgent analyzes market news and sentiment to provide trading recommendations.
// Requirements: 11.4, 36.1-36.7
type NewsAgent struct {
	BaseAgent
	llmClient LLMClient
	webSearch WebSearchClient
}

// NewNewsAgent creates a new news analysis agent.
func NewNewsAgent(llmClient LLMClient, webSearch WebSearchClient, weight float64) *NewsAgent {
	return &NewsAgent{
		BaseAgent: NewBaseAgent("news", weight),
		llmClient: llmClient,
		webSearch: webSearch,
	}
}

// Analyze performs news and sentiment analysis.
func (a *NewsAgent) Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResult, error) {
	// Fetch news if not provided
	news := req.News
	if len(news) == 0 && a.webSearch != nil {
		var err error
		news, err = a.fetchNews(ctx, req.Symbol)
		if err != nil {
			news = []NewsItem{}
		}
	}

	// Fetch upcoming events
	events, _ := a.fetchEvents(ctx, req.Symbol)

	// If no LLM client, perform rule-based analysis
	if a.llmClient == nil {
		return a.ruleBasedAnalysis(req, news, events)
	}

	// Build context for LLM
	analysisContext := a.buildAnalysisContext(req, news, events)

	systemPrompt := `You are a news and sentiment analyst for Indian stock markets.
Analyze the provided news and events to assess market sentiment and potential impact on the stock.
Consider: news sentiment, upcoming events, corporate announcements, and market mood.
Your response must be in the following exact format:
RECOMMENDATION: BUY|SELL|HOLD
CONFIDENCE: <number 0-100>
ENTRY: <price or N/A>
STOPLOSS: <price or N/A>
TARGET1: <price or N/A>
TARGET2: <price or N/A>
TARGET3: <price or N/A>
REASONING: <your analysis in one paragraph>`

	response, err := a.llmClient.CompleteWithSystem(ctx, systemPrompt, analysisContext)
	if err != nil {
		return a.ruleBasedAnalysis(req, news, events)
	}

	return a.parseResponse(response, req)
}

// CorporateEvent represents an upcoming corporate event.
type CorporateEvent struct {
	Symbol      string
	EventType   string // earnings, dividend, agm, bonus, split, rights
	Date        time.Time
	Description string
	Impact      string // positive, negative, neutral
}

// fetchNews fetches recent news for a symbol.
func (a *NewsAgent) fetchNews(ctx context.Context, symbol string) ([]NewsItem, error) {
	if a.webSearch == nil {
		return nil, fmt.Errorf("web search client not configured")
	}

	queries := []string{
		fmt.Sprintf("%s stock news India today", symbol),
		fmt.Sprintf("%s NSE BSE latest news", symbol),
	}

	var news []NewsItem
	for _, query := range queries {
		results, err := a.webSearch.Search(ctx, query, 5)
		if err != nil {
			continue
		}

		for _, r := range results {
			item := NewsItem{
				Title:       r.Title,
				URL:         r.URL,
				Summary:     truncateString(r.Content, 500),
				Relevance:   r.Score,
				PublishedAt: time.Now(), // Would need actual date parsing
				Timestamp:   time.Now(),
			}
			// Estimate sentiment from content
			item.Sentiment = a.estimateSentiment(r.Content)
			news = append(news, item)
		}
	}

	return news, nil
}

// fetchEvents fetches upcoming corporate events.
func (a *NewsAgent) fetchEvents(ctx context.Context, symbol string) ([]CorporateEvent, error) {
	if a.webSearch == nil {
		return nil, fmt.Errorf("web search client not configured")
	}

	query := fmt.Sprintf("%s upcoming events earnings dividend AGM India", symbol)
	results, err := a.webSearch.Search(ctx, query, 3)
	if err != nil {
		return nil, err
	}

	var events []CorporateEvent
	for _, r := range results {
		// Simple event extraction - in production, use more sophisticated parsing
		content := strings.ToLower(r.Content)
		
		if strings.Contains(content, "earnings") || strings.Contains(content, "results") {
			events = append(events, CorporateEvent{
				Symbol:      symbol,
				EventType:   "earnings",
				Description: truncateString(r.Content, 200),
			})
		}
		if strings.Contains(content, "dividend") {
			events = append(events, CorporateEvent{
				Symbol:      symbol,
				EventType:   "dividend",
				Description: truncateString(r.Content, 200),
			})
		}
		if strings.Contains(content, "agm") || strings.Contains(content, "annual general meeting") {
			events = append(events, CorporateEvent{
				Symbol:      symbol,
				EventType:   "agm",
				Description: truncateString(r.Content, 200),
			})
		}
	}

	return events, nil
}

// estimateSentiment estimates sentiment from text content.
func (a *NewsAgent) estimateSentiment(content string) float64 {
	content = strings.ToLower(content)

	positiveWords := []string{
		"surge", "rally", "gain", "profit", "growth", "bullish", "upgrade",
		"beat", "exceed", "strong", "positive", "outperform", "buy",
		"record", "high", "boost", "improve", "success", "optimistic",
	}

	negativeWords := []string{
		"fall", "drop", "decline", "loss", "bearish", "downgrade",
		"miss", "weak", "negative", "underperform", "sell", "concern",
		"low", "cut", "reduce", "warning", "risk", "pessimistic",
	}

	var positiveCount, negativeCount int
	for _, word := range positiveWords {
		positiveCount += strings.Count(content, word)
	}
	for _, word := range negativeWords {
		negativeCount += strings.Count(content, word)
	}

	total := positiveCount + negativeCount
	if total == 0 {
		return 0
	}

	// Return sentiment from -1 to 1
	return float64(positiveCount-negativeCount) / float64(total)
}

// buildAnalysisContext creates context for LLM analysis.
func (a *NewsAgent) buildAnalysisContext(req AnalysisRequest, news []NewsItem, events []CorporateEvent) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Symbol: %s\n", req.Symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.2f\n\n", req.CurrentPrice))

	// News
	if len(news) > 0 {
		sb.WriteString("Recent News:\n")
		for i, n := range news {
			if i >= 5 {
				break
			}
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, n.Title))
			if n.Summary != "" {
				sb.WriteString(fmt.Sprintf("     Summary: %s\n", truncateString(n.Summary, 200)))
			}
			sb.WriteString(fmt.Sprintf("     Sentiment: %.2f\n", n.Sentiment))
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("No recent news available.\n\n")
	}

	// Events
	if len(events) > 0 {
		sb.WriteString("Upcoming Events:\n")
		for _, e := range events {
			sb.WriteString(fmt.Sprintf("  - %s: %s\n", e.EventType, e.Description))
		}
		sb.WriteString("\n")
	}

	// Market context
	if req.MarketState != nil {
		sb.WriteString("Market Context:\n")
		sb.WriteString(fmt.Sprintf("  Nifty: %.2f (%.2f%%)\n", req.MarketState.NiftyLevel, req.MarketState.NiftyChange))
		sb.WriteString(fmt.Sprintf("  VIX: %.2f\n", req.MarketState.VIXLevel))
		sb.WriteString(fmt.Sprintf("  Market Trend: %s\n", req.MarketState.MarketTrend))
		
		// Market breadth
		if req.MarketState.Breadth.Advances > 0 || req.MarketState.Breadth.Declines > 0 {
			sb.WriteString(fmt.Sprintf("  Advances/Declines: %d/%d\n", 
				req.MarketState.Breadth.Advances, req.MarketState.Breadth.Declines))
		}
	}

	return sb.String()
}

// ruleBasedAnalysis performs analysis without LLM.
func (a *NewsAgent) ruleBasedAnalysis(req AnalysisRequest, news []NewsItem, events []CorporateEvent) (*AnalysisResult, error) {
	result := a.CreateResult(Hold, 50, "")

	var score float64
	var reasons []string

	// Analyze news sentiment
	if len(news) > 0 {
		var totalSentiment float64
		var weightedSentiment float64
		for i, n := range news {
			// More recent news has higher weight
			weight := 1.0 / float64(i+1)
			weightedSentiment += n.Sentiment * weight
			totalSentiment += weight
		}

		if totalSentiment > 0 {
			avgSentiment := weightedSentiment / totalSentiment
			score += avgSentiment * 50 // Scale to -50 to +50

			if avgSentiment > 0.3 {
				reasons = append(reasons, "positive news sentiment")
			} else if avgSentiment < -0.3 {
				reasons = append(reasons, "negative news sentiment")
			} else {
				reasons = append(reasons, "neutral news sentiment")
			}
		}
	}

	// Analyze events
	for _, e := range events {
		switch e.EventType {
		case "earnings":
			// Earnings can go either way, add uncertainty
			reasons = append(reasons, "upcoming earnings event")
		case "dividend":
			score += 10
			reasons = append(reasons, "dividend announcement")
		case "bonus", "split":
			score += 15
			reasons = append(reasons, fmt.Sprintf("upcoming %s", e.EventType))
		}
	}

	// Market context influence
	if req.MarketState != nil {
		// VIX influence
		if req.MarketState.VIXLevel > 25 {
			score -= 10
			reasons = append(reasons, "high market volatility")
		} else if req.MarketState.VIXLevel < 15 {
			score += 5
			reasons = append(reasons, "low market volatility")
		}

		// Market trend influence
		switch req.MarketState.MarketTrend {
		case "BULLISH":
			score += 10
		case "BEARISH":
			score -= 10
		}

		// Breadth influence
		if req.MarketState.Breadth.AdvDecRatio > 1.5 {
			score += 10
			reasons = append(reasons, "positive market breadth")
		} else if req.MarketState.Breadth.AdvDecRatio < 0.67 {
			score -= 10
			reasons = append(reasons, "negative market breadth")
		}
	}

	// Determine recommendation
	if score >= 25 {
		result.Recommendation = Buy
	} else if score <= -25 {
		result.Recommendation = Sell
	} else {
		result.Recommendation = Hold
	}

	// Calculate confidence - news-based analysis typically has lower confidence
	result.Confidence = ClampConfidence(40 + abs(score)/3)

	// Build reasoning
	if len(reasons) > 0 {
		result.Reasoning = fmt.Sprintf("News and sentiment analysis: %s.", strings.Join(reasons, ", "))
	} else {
		result.Reasoning = "No significant news or events to analyze."
	}

	// Set trade levels for BUY/SELL
	if result.Recommendation != Hold && req.CurrentPrice > 0 {
		a.calculateTradeLevels(result, req)
	}

	result.Timestamp = time.Now()
	return result, nil
}

// calculateTradeLevels sets entry, SL, and targets.
func (a *NewsAgent) calculateTradeLevels(result *AnalysisResult, req AnalysisRequest) {
	price := req.CurrentPrice

	if result.Recommendation == Buy {
		result.EntryPrice = price
		result.StopLoss = price * 0.97 // 3% stop-loss for news-driven trades
		result.Targets = []float64{
			price * 1.03, // 3%
			price * 1.05, // 5%
			price * 1.08, // 8%
		}
	} else if result.Recommendation == Sell {
		result.EntryPrice = price
		result.StopLoss = price * 1.03
		result.Targets = []float64{
			price * 0.97,
			price * 0.95,
			price * 0.92,
		}
	}

	result.RiskReward = CalculateRiskReward(
		result.EntryPrice,
		result.StopLoss,
		result.Targets,
		result.Recommendation == Buy,
	)
}

// parseResponse parses LLM response into AnalysisResult.
func (a *NewsAgent) parseResponse(response string, req AnalysisRequest) (*AnalysisResult, error) {
	result := a.CreateResult(Hold, 50, "")

	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "RECOMMENDATION:") {
			rec := strings.TrimSpace(strings.TrimPrefix(line, "RECOMMENDATION:"))
			switch strings.ToUpper(rec) {
			case "BUY":
				result.Recommendation = Buy
			case "SELL":
				result.Recommendation = Sell
			default:
				result.Recommendation = Hold
			}
		} else if strings.HasPrefix(line, "CONFIDENCE:") {
			fmt.Sscanf(strings.TrimPrefix(line, "CONFIDENCE:"), "%f", &result.Confidence)
			result.Confidence = ClampConfidence(result.Confidence)
		} else if strings.HasPrefix(line, "ENTRY:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "ENTRY:"))
			if val != "N/A" {
				fmt.Sscanf(val, "%f", &result.EntryPrice)
			}
		} else if strings.HasPrefix(line, "STOPLOSS:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "STOPLOSS:"))
			if val != "N/A" {
				fmt.Sscanf(val, "%f", &result.StopLoss)
			}
		} else if strings.HasPrefix(line, "TARGET1:") || strings.HasPrefix(line, "TARGET2:") || strings.HasPrefix(line, "TARGET3:") {
			prefix := line[:8]
			val := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if val != "N/A" {
				var t float64
				fmt.Sscanf(val, "%f", &t)
				if t > 0 {
					result.Targets = append(result.Targets, t)
				}
			}
		} else if strings.HasPrefix(line, "REASONING:") {
			result.Reasoning = strings.TrimSpace(strings.TrimPrefix(line, "REASONING:"))
		}
	}

	if result.EntryPrice > 0 && result.StopLoss > 0 && len(result.Targets) > 0 {
		result.RiskReward = CalculateRiskReward(
			result.EntryPrice,
			result.StopLoss,
			result.Targets,
			result.Recommendation == Buy,
		)
	}

	if result.Reasoning == "" {
		return a.ruleBasedAnalysis(req, req.News, nil)
	}

	result.Timestamp = time.Now()
	return result, nil
}

// FetchNews fetches news for a symbol (public method for external use).
func (a *NewsAgent) FetchNews(ctx context.Context, symbol string) ([]NewsItem, error) {
	return a.fetchNews(ctx, symbol)
}

// FetchEvents fetches upcoming events for a symbol.
func (a *NewsAgent) FetchEvents(ctx context.Context, symbol string) ([]CorporateEvent, error) {
	return a.fetchEvents(ctx, symbol)
}

// Helper function
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
