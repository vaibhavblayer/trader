package cli

import (
	"testing"
	"time"

	"zerodha-trader/internal/agents"
)

func TestEvidenceAwareCandidateScoreRewardsCitedPositiveEvidence(t *testing.T) {
	row := backtestGridResult{
		Verdict:             "WATCH",
		Trades:              18,
		ValidationTrades:    6,
		ReturnPct:           4,
		ValidationReturnPct: 2,
		TrainReturnPct:      2,
		ProfitFactor:        1.4,
		Expectancy:          500,
		MaxDrawdownPct:      4,
		SignalBars:          100,
		DirectionalSignals:  8,
		SignalRatePct:       8,
		TradeConversionPct:  75,
		LatestSignal:        "BUY",
		Regimes:             []backtestRegimeTradeStat{{Regime: "range", Trades: 18, Expectancy: 500}},
	}
	discovery := ScanResult{Symbol: "HDFCBANK", Change: 1.5, Volume: 2.2, ATRPct: 1.8, Candles: 80, LastCandleAgeMinutes: 10}
	positiveEvidence := candidateEvidenceResult{Report: &agents.ResearchEvidenceReport{
		Sentiment:      "positive",
		SentimentScore: 0.6,
		Confidence:     80,
		Catalysts:      []agents.ResearchEvidencePoint{{Text: "earnings beat"}},
		Sources:        []agents.ResearchEvidenceSource{{URL: "https://example.com/a"}, {URL: "https://example.com/b"}, {URL: "https://example.com/c"}},
		GeneratedAt:    time.Now(),
	}}
	negativeEvidence := candidateEvidenceResult{Report: &agents.ResearchEvidenceReport{
		Sentiment:      "negative",
		SentimentScore: -0.6,
		Confidence:     80,
		Risks:          []agents.ResearchEvidencePoint{{Text: "margin pressure"}},
		EventRisks:     []agents.ResearchEvidencePoint{{Text: "governance event"}},
		Sources:        []agents.ResearchEvidenceSource{{URL: "https://example.com/a"}},
		GeneratedAt:    time.Now(),
	}}

	positiveScore, positiveComponents, _ := evidenceAwareCandidateScore(row, discovery, positiveEvidence)
	negativeScore, negativeComponents, _ := evidenceAwareCandidateScore(row, discovery, negativeEvidence)
	if positiveComponents.Evidence <= negativeComponents.Evidence {
		t.Fatalf("expected positive evidence to score higher: positive %.2f negative %.2f", positiveComponents.Evidence, negativeComponents.Evidence)
	}
	if positiveScore <= negativeScore {
		t.Fatalf("expected positive candidate score to be higher: positive %.2f negative %.2f", positiveScore, negativeScore)
	}
}

func TestApplyEvidenceAwareCandidateScoresAnnotatesRows(t *testing.T) {
	rows := []backtestGridResult{{Symbol: "FEDERALBNK", Verdict: "WATCH", Trades: 3, Expectancy: 100, SignalBars: 20, DirectionalSignals: 1, SignalRatePct: 5}}
	discovered := []ScanResult{{Symbol: "FEDERALBNK", Volume: 3, Change: 1, ATRPct: 1, Candles: 50}}
	evidence := map[string]candidateEvidenceResult{
		"FEDERALBNK": {Report: &agents.ResearchEvidenceReport{Sentiment: "mixed", Confidence: 70, Sources: []agents.ResearchEvidenceSource{{URL: "https://example.com"}}}},
	}

	applyEvidenceAwareCandidateScores(rows, discovered, evidence)
	if rows[0].CandidateScore == 0 || rows[0].ScoreComponents.Backtest == 0 {
		t.Fatalf("expected score annotations: %#v", rows[0])
	}
	if rows[0].EvidenceSentiment != "mixed" || rows[0].EvidenceSources != 1 {
		t.Fatalf("expected evidence metadata: %#v", rows[0])
	}
}
