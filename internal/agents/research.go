// Package agents provides AI agent implementations for trading decisions.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ResearchAgent analyzes company fundamentals and market conditions using web search.
// Requirements: 11.2, 35.1-35.11
type ResearchAgent struct {
	BaseAgent
	llmClient   LLMClient
	webSearch   WebSearchClient
	cache       *ResearchCache
	cacheTTL    time.Duration
}

// WebSearchClient defines the interface for web search operations (e.g., Tavily API).
type WebSearchClient interface {
	// Search performs a web search and returns results.
	Search(ctx context.Context, query string, maxResults int) ([]SearchResult, error)
}

// SearchResult represents a single web search result.
type SearchResult struct {
	Title   string
	URL     string
	Content string
	Score   float64
}

// ResearchCache provides caching for research results.
type ResearchCache struct {
	mu      sync.RWMutex
	reports map[string]*cachedReport
}

type cachedReport struct {
	report    *ResearchReport
	timestamp time.Time
}

// NewResearchCache creates a new research cache.
func NewResearchCache() *ResearchCache {
	return &ResearchCache{
		reports: make(map[string]*cachedReport),
	}
}

// Get retrieves a cached report if it exists and is not expired.
func (c *ResearchCache) Get(symbol string, ttl time.Duration) *ResearchReport {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.reports[symbol]
	if !ok {
		return nil
	}

	if time.Since(cached.timestamp) > ttl {
		return nil
	}

	return cached.report
}

// Set stores a report in the cache.
func (c *ResearchCache) Set(symbol string, report *ResearchReport) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.reports[symbol] = &cachedReport{
		report:    report,
		timestamp: time.Now(),
	}
}

// NewResearchAgent creates a new research agent.
func NewResearchAgent(llmClient LLMClient, webSearch WebSearchClient, weight float64) *ResearchAgent {
	return &ResearchAgent{
		BaseAgent: NewBaseAgent("research", weight),
		llmClient: llmClient,
		webSearch: webSearch,
		cache:     NewResearchCache(),
		cacheTTL:  4 * time.Hour, // Cache research for 4 hours
	}
}

// Analyze performs fundamental research analysis.
func (a *ResearchAgent) Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResult, error) {
	// Check cache first
	var report *ResearchReport
	if req.Research != nil {
		report = req.Research
	} else {
		report = a.cache.Get(req.Symbol, a.cacheTTL)
	}

	// Fetch fresh research if needed
	if report == nil && a.webSearch != nil {
		var err error
		report, err = a.fetchResearch(ctx, req.Symbol)
		if err != nil {
			// Continue with limited analysis
			report = &ResearchReport{
				Symbol:      req.Symbol,
				LastUpdated: time.Now(),
			}
		} else {
			a.cache.Set(req.Symbol, report)
		}
	}

	// If no LLM client, perform rule-based analysis
	if a.llmClient == nil {
		return a.ruleBasedAnalysis(req, report)
	}

	// Build context for LLM
	analysisContext := a.buildAnalysisContext(req, report)

	systemPrompt := `You are a fundamental research analyst for Indian stock markets.
Analyze the provided research data and provide a trading recommendation based on fundamentals.
Consider: valuation metrics, growth prospects, analyst ratings, sector trends, and risks.
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
		return a.ruleBasedAnalysis(req, report)
	}

	return a.parseResponse(response, req, report)
}

// fetchResearch fetches research data using web search.
func (a *ResearchAgent) fetchResearch(ctx context.Context, symbol string) (*ResearchReport, error) {
	report := &ResearchReport{
		Symbol:      symbol,
		LastUpdated: time.Now(),
	}

	// Search for company fundamentals
	queries := []string{
		fmt.Sprintf("%s NSE stock fundamentals PE ratio market cap", symbol),
		fmt.Sprintf("%s stock analyst rating price target India", symbol),
		fmt.Sprintf("%s company news sector outlook India", symbol),
	}

	var allResults []SearchResult
	for _, query := range queries {
		results, err := a.webSearch.Search(ctx, query, 3)
		if err != nil {
			continue
		}
		allResults = append(allResults, results...)
	}

	// Extract information from search results
	a.extractResearchData(report, allResults)

	return report, nil
}

// extractResearchData extracts structured data from search results.
func (a *ResearchAgent) extractResearchData(report *ResearchReport, results []SearchResult) {
	// Combine all content for analysis
	var contentBuilder strings.Builder
	for _, r := range results {
		contentBuilder.WriteString(r.Content)
		contentBuilder.WriteString("\n")
	}
	content := contentBuilder.String()

	// Extract key highlights from content
	if len(content) > 0 {
		// Simple extraction - in production, use LLM for better extraction
		sentences := strings.Split(content, ".")
		for i, s := range sentences {
			if i >= 5 {
				break
			}
			s = strings.TrimSpace(s)
			if len(s) > 20 && len(s) < 200 {
				report.KeyHighlights = append(report.KeyHighlights, s)
			}
		}
	}
}

// buildAnalysisContext creates context for LLM analysis.
func (a *ResearchAgent) buildAnalysisContext(req AnalysisRequest, report *ResearchReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Symbol: %s\n", req.Symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.2f\n\n", req.CurrentPrice))

	if report != nil {
		sb.WriteString("Fundamental Data:\n")
		if report.CompanyName != "" {
			sb.WriteString(fmt.Sprintf("  Company: %s\n", report.CompanyName))
		}
		if report.Sector != "" {
			sb.WriteString(fmt.Sprintf("  Sector: %s\n", report.Sector))
		}
		if report.Industry != "" {
			sb.WriteString(fmt.Sprintf("  Industry: %s\n", report.Industry))
		}
		if report.MarketCap > 0 {
			sb.WriteString(fmt.Sprintf("  Market Cap: %.2f Cr\n", report.MarketCap/10000000))
		}
		if report.PE > 0 {
			sb.WriteString(fmt.Sprintf("  P/E Ratio: %.2f\n", report.PE))
		}
		if report.PB > 0 {
			sb.WriteString(fmt.Sprintf("  P/B Ratio: %.2f\n", report.PB))
		}
		if report.ROE > 0 {
			sb.WriteString(fmt.Sprintf("  ROE: %.2f%%\n", report.ROE))
		}
		if report.DebtToEquity > 0 {
			sb.WriteString(fmt.Sprintf("  Debt/Equity: %.2f\n", report.DebtToEquity))
		}
		if report.DividendYield > 0 {
			sb.WriteString(fmt.Sprintf("  Dividend Yield: %.2f%%\n", report.DividendYield))
		}
		if report.RevenueGrowth != 0 {
			sb.WriteString(fmt.Sprintf("  Revenue Growth: %.2f%%\n", report.RevenueGrowth))
		}
		if report.ProfitGrowth != 0 {
			sb.WriteString(fmt.Sprintf("  Profit Growth: %.2f%%\n", report.ProfitGrowth))
		}
		sb.WriteString("\n")

		if report.AnalystRating != "" {
			sb.WriteString(fmt.Sprintf("Analyst Rating: %s\n", report.AnalystRating))
		}
		if len(report.PriceTargets) > 0 {
			sb.WriteString(fmt.Sprintf("Price Targets: %v\n", report.PriceTargets))
		}
		if report.AvgPriceTarget > 0 {
			sb.WriteString(fmt.Sprintf("Average Price Target: %.2f\n", report.AvgPriceTarget))
		}
		sb.WriteString("\n")

		if len(report.KeyHighlights) > 0 {
			sb.WriteString("Key Highlights:\n")
			for _, h := range report.KeyHighlights {
				sb.WriteString(fmt.Sprintf("  - %s\n", h))
			}
			sb.WriteString("\n")
		}

		if len(report.Risks) > 0 {
			sb.WriteString("Risks:\n")
			for _, r := range report.Risks {
				sb.WriteString(fmt.Sprintf("  - %s\n", r))
			}
			sb.WriteString("\n")
		}

		if len(report.Catalysts) > 0 {
			sb.WriteString("Catalysts:\n")
			for _, c := range report.Catalysts {
				sb.WriteString(fmt.Sprintf("  - %s\n", c))
			}
		}
	}

	// Market context
	if req.MarketState != nil {
		sb.WriteString("\nMarket Context:\n")
		sb.WriteString(fmt.Sprintf("  Nifty: %.2f (%.2f%%)\n", req.MarketState.NiftyLevel, req.MarketState.NiftyChange))
		sb.WriteString(fmt.Sprintf("  Market Trend: %s\n", req.MarketState.MarketTrend))
	}

	return sb.String()
}

// ruleBasedAnalysis performs analysis without LLM.
func (a *ResearchAgent) ruleBasedAnalysis(req AnalysisRequest, report *ResearchReport) (*AnalysisResult, error) {
	result := a.CreateResult(Hold, 50, "")

	var score float64
	var reasons []string

	if report != nil {
		// Valuation analysis
		if report.PE > 0 {
			if report.PE < 15 {
				score += 20
				reasons = append(reasons, "attractive P/E ratio")
			} else if report.PE > 40 {
				score -= 20
				reasons = append(reasons, "high P/E ratio")
			}
		}

		if report.PB > 0 {
			if report.PB < 2 {
				score += 15
				reasons = append(reasons, "low P/B ratio")
			} else if report.PB > 5 {
				score -= 15
				reasons = append(reasons, "high P/B ratio")
			}
		}

		// Growth analysis
		if report.RevenueGrowth > 15 {
			score += 15
			reasons = append(reasons, "strong revenue growth")
		} else if report.RevenueGrowth < 0 {
			score -= 15
			reasons = append(reasons, "declining revenue")
		}

		if report.ProfitGrowth > 20 {
			score += 15
			reasons = append(reasons, "strong profit growth")
		} else if report.ProfitGrowth < 0 {
			score -= 15
			reasons = append(reasons, "declining profits")
		}

		// Quality metrics
		if report.ROE > 15 {
			score += 10
			reasons = append(reasons, "good ROE")
		}

		if report.DebtToEquity < 0.5 {
			score += 10
			reasons = append(reasons, "low debt")
		} else if report.DebtToEquity > 2 {
			score -= 10
			reasons = append(reasons, "high debt")
		}

		// Analyst rating
		switch strings.ToUpper(report.AnalystRating) {
		case "BUY", "STRONG BUY":
			score += 20
			reasons = append(reasons, "positive analyst rating")
		case "SELL", "STRONG SELL":
			score -= 20
			reasons = append(reasons, "negative analyst rating")
		}

		// Price target analysis
		if report.AvgPriceTarget > 0 && req.CurrentPrice > 0 {
			upside := (report.AvgPriceTarget - req.CurrentPrice) / req.CurrentPrice * 100
			if upside > 20 {
				score += 15
				reasons = append(reasons, fmt.Sprintf("%.1f%% upside to target", upside))
			} else if upside < -10 {
				score -= 15
				reasons = append(reasons, fmt.Sprintf("%.1f%% downside to target", upside))
			}
		}
	}

	// Determine recommendation
	if score >= 30 {
		result.Recommendation = Buy
	} else if score <= -30 {
		result.Recommendation = Sell
	} else {
		result.Recommendation = Hold
	}

	// Calculate confidence
	result.Confidence = ClampConfidence(50 + abs(score)/2)

	// Build reasoning
	if len(reasons) > 0 {
		result.Reasoning = fmt.Sprintf("Fundamental analysis: %s.", strings.Join(reasons, ", "))
	} else {
		result.Reasoning = "Insufficient fundamental data for detailed analysis."
	}

	// Set trade levels for BUY/SELL
	if result.Recommendation != Hold && req.CurrentPrice > 0 {
		a.calculateTradeLevels(result, req, report)
	}

	result.Timestamp = time.Now()
	return result, nil
}

// calculateTradeLevels sets entry, SL, and targets based on fundamentals.
func (a *ResearchAgent) calculateTradeLevels(result *AnalysisResult, req AnalysisRequest, report *ResearchReport) {
	price := req.CurrentPrice

	if result.Recommendation == Buy {
		result.EntryPrice = price
		result.StopLoss = price * 0.95 // 5% stop-loss for fundamental trades

		// Use analyst targets if available
		if report != nil && len(report.PriceTargets) > 0 {
			result.Targets = report.PriceTargets
		} else if report != nil && report.AvgPriceTarget > 0 {
			result.Targets = []float64{
				price + (report.AvgPriceTarget-price)*0.5,
				report.AvgPriceTarget,
				report.AvgPriceTarget * 1.1,
			}
		} else {
			result.Targets = []float64{
				price * 1.10, // 10%
				price * 1.20, // 20%
				price * 1.30, // 30%
			}
		}
	} else if result.Recommendation == Sell {
		result.EntryPrice = price
		result.StopLoss = price * 1.05 // 5% stop-loss

		result.Targets = []float64{
			price * 0.90, // 10%
			price * 0.80, // 20%
			price * 0.70, // 30%
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
func (a *ResearchAgent) parseResponse(response string, req AnalysisRequest, report *ResearchReport) (*AnalysisResult, error) {
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
		return a.ruleBasedAnalysis(req, report)
	}

	result.Timestamp = time.Now()
	return result, nil
}

// GetCachedReport returns a cached research report if available.
func (a *ResearchAgent) GetCachedReport(symbol string) *ResearchReport {
	return a.cache.Get(symbol, a.cacheTTL)
}

// GenerateReport generates a structured research report for a symbol.
func (a *ResearchAgent) GenerateReport(ctx context.Context, symbol string) (*ResearchReport, error) {
	// Check cache
	if cached := a.cache.Get(symbol, a.cacheTTL); cached != nil {
		return cached, nil
	}

	// Fetch fresh research
	report, err := a.fetchResearch(ctx, symbol)
	if err != nil {
		return nil, err
	}

	a.cache.Set(symbol, report)
	return report, nil
}

// ResearchReportJSON returns the research report as JSON.
func (r *ResearchReport) JSON() (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
