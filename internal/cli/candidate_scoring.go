package cli

import (
	"fmt"
	"math"
	"strings"

	"zerodha-trader/internal/agents"
)

type candidateScoreComponents struct {
	Backtest  float64 `json:"backtest"`
	Signals   float64 `json:"signals"`
	Regime    float64 `json:"regime"`
	Discovery float64 `json:"discovery"`
	Evidence  float64 `json:"evidence"`
}

type candidateEvidenceResult struct {
	Report *agents.ResearchEvidenceReport `json:"report,omitempty"`
	Error  string                         `json:"error,omitempty"`
	Score  float64                        `json:"score"`
}

func applyEvidenceAwareCandidateScores(results []backtestGridResult, discovered []ScanResult, evidence map[string]candidateEvidenceResult) {
	discoveryBySymbol := make(map[string]ScanResult, len(discovered))
	for _, result := range discovered {
		discoveryBySymbol[strings.ToUpper(result.Symbol)] = result
	}
	for i := range results {
		discovery := discoveryBySymbol[strings.ToUpper(results[i].Symbol)]
		ev := evidence[strings.ToUpper(results[i].Symbol)]
		score, components, reason := evidenceAwareCandidateScore(results[i], discovery, ev)
		results[i].CandidateScore = score
		results[i].CandidateScoreReason = reason
		results[i].ScoreComponents = components
		results[i].EvidenceScore = components.Evidence
		if ev.Report != nil {
			results[i].EvidenceSentiment = ev.Report.Sentiment
			results[i].EvidenceConfidence = ev.Report.Confidence
			results[i].EvidenceSources = len(ev.Report.Sources)
		}
		if ev.Error != "" {
			results[i].EvidenceError = ev.Error
		}
	}
}

func evidenceAwareCandidateScore(row backtestGridResult, discovery ScanResult, evidence candidateEvidenceResult) (float64, candidateScoreComponents, string) {
	components := candidateScoreComponents{
		Backtest:  scoreBacktestQuality(row),
		Signals:   scoreSignalActivity(row),
		Regime:    scoreRegimeExpectancy(row),
		Discovery: scoreDiscoveryActivity(discovery),
		Evidence:  scoreResearchEvidence(evidence),
	}
	score := components.Backtest*0.40 +
		components.Signals*0.15 +
		components.Regime*0.15 +
		components.Discovery*0.10 +
		components.Evidence*0.20
	return clampScore(score), components, candidateScoreReason(components, row, evidence)
}

func scoreBacktestQuality(row backtestGridResult) float64 {
	score := 0.0
	switch row.Verdict {
	case "PASS":
		score += 35
	case "WATCH":
		score += 22
	case "REJECT":
		score += 5
	}
	if row.Trades > 0 {
		score += clampScore(float64(row.Trades) / 30 * 15)
	}
	if row.ValidationTrades > 0 {
		score += clampScore(float64(row.ValidationTrades) / 10 * 12)
	}
	score += clampScore(row.ReturnPct+10) * 0.10
	score += clampScore(row.ValidationReturnPct+10) * 0.12
	score += clampScore((math.Min(row.ProfitFactor, 3)-1)*18 + 18)
	if row.Expectancy > 0 {
		score += 10
	}
	score -= clampScore(row.MaxDrawdownPct) * 0.5
	return clampScore(score)
}

func scoreSignalActivity(row backtestGridResult) float64 {
	if row.SignalBars == 0 {
		return 35
	}
	score := 35.0
	if row.DirectionalSignals > 0 {
		score += 20
	}
	rate := row.SignalRatePct
	switch {
	case rate >= 1 && rate <= 20:
		score += 25
	case rate > 20 && rate <= 35:
		score += 12
	case rate > 0 && rate < 1:
		score += 8
	}
	if row.TradeConversionPct >= 50 {
		score += 15
	} else if row.TradeConversionPct > 0 {
		score += 8
	}
	if row.LatestSignal != "" && row.LatestSignal != "HOLD" {
		score += 5
	}
	if row.DirectionalSignals > 0 && row.DirectionalSignals < 3 {
		score = math.Min(score, 65)
	}
	return clampScore(score)
}

func scoreRegimeExpectancy(row backtestGridResult) float64 {
	if len(row.Regimes) == 0 {
		if row.Expectancy > 0 {
			return 55
		}
		return 40
	}
	totalTrades := 0
	positiveTrades := 0
	weightedExpectancy := 0.0
	for _, regime := range row.Regimes {
		totalTrades += regime.Trades
		if regime.Expectancy > 0 {
			positiveTrades += regime.Trades
		}
		weightedExpectancy += regime.Expectancy * float64(regime.Trades)
	}
	if totalTrades == 0 {
		return 40
	}
	positiveRatio := float64(positiveTrades) / float64(totalTrades)
	avgExpectancy := weightedExpectancy / float64(totalTrades)
	score := positiveRatio * 70
	if avgExpectancy > 0 {
		score += 20
	}
	if avgExpectancy < 0 {
		score -= 20
	}
	sampleConfidence := math.Min(float64(totalTrades)/10, 1)
	return clampScore(40 + (score-40)*sampleConfidence)
}

func scoreDiscoveryActivity(discovery ScanResult) float64 {
	if discovery.Symbol == "" {
		return 45
	}
	score := 35.0
	score += math.Min(math.Abs(discovery.Change)*8, 20)
	score += math.Min(discovery.Volume*6, 20)
	score += math.Min(discovery.ATRPct*8, 15)
	if discovery.Candles >= 80 {
		score += 10
	} else if discovery.Candles >= 30 {
		score += 5
	}
	if discovery.LastCandleAgeMinutes > 0 && discovery.LastCandleAgeMinutes <= 60 {
		score += 10
	}
	return clampScore(score)
}

func scoreResearchEvidence(evidence candidateEvidenceResult) float64 {
	if evidence.Error != "" {
		return 42
	}
	report := evidence.Report
	if report == nil {
		return 50
	}
	score := 50.0 + report.SentimentScore*25
	switch strings.ToLower(report.Sentiment) {
	case "positive":
		score += 12
	case "negative":
		score -= 12
	case "mixed":
		score -= 2
	}
	score += math.Min(float64(len(report.Catalysts))*4, 16)
	score -= math.Min(float64(len(report.Risks))*4, 16)
	score -= math.Min(float64(len(report.EventRisks))*5, 20)
	if len(report.Sources) >= 3 {
		score += 8
	}
	confidence := report.Confidence
	if confidence <= 0 {
		confidence = 50
	}
	score = 50 + (score-50)*(confidence/100)
	return clampScore(score)
}

func candidateScoreReason(components candidateScoreComponents, row backtestGridResult, evidence candidateEvidenceResult) string {
	reasons := []string{
		fmt.Sprintf("backtest=%.1f", components.Backtest),
		fmt.Sprintf("signals=%.1f", components.Signals),
		fmt.Sprintf("regime=%.1f", components.Regime),
		fmt.Sprintf("discovery=%.1f", components.Discovery),
		fmt.Sprintf("evidence=%.1f", components.Evidence),
	}
	if row.Verdict != "" {
		reasons = append(reasons, "verdict="+row.Verdict)
	}
	if evidence.Report != nil && evidence.Report.Sentiment != "" {
		reasons = append(reasons, "sentiment="+evidence.Report.Sentiment)
	}
	if evidence.Error != "" {
		reasons = append(reasons, "evidence_error")
	}
	return strings.Join(reasons, ";")
}

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
