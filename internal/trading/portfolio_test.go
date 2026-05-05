package trading

import (
	"context"
	"strings"
	"testing"
)

func TestPortfolioPlaceholderAnalyticsFailClosed(t *testing.T) {
	analyzer := &DefaultPortfolioAnalyzer{}
	ctx := context.Background()

	if greeks, err := analyzer.GetPortfolioGreeks(ctx); err == nil || greeks != nil || !strings.Contains(err.Error(), "data-backed option Greeks") {
		t.Fatalf("expected data-backed Greeks error, got greeks=%#v err=%v", greeks, err)
	}
	if beta, err := analyzer.GetPortfolioBeta(ctx); err == nil || beta != 0 || !strings.Contains(err.Error(), "historical portfolio and benchmark returns") {
		t.Fatalf("expected data-backed beta error, got beta=%v err=%v", beta, err)
	}
	if var95, err := analyzer.GetVaR(ctx, 0.95); err == nil || var95 != 0 || !strings.Contains(err.Error(), "historical portfolio returns") {
		t.Fatalf("expected data-backed VaR error, got var=%v err=%v", var95, err)
	}
	if hedges, err := analyzer.SuggestHedges(ctx); err == nil || hedges != nil || !strings.Contains(err.Error(), "data-backed beta") {
		t.Fatalf("expected data-backed hedge error, got hedges=%#v err=%v", hedges, err)
	}
	if metrics, err := analyzer.GetRiskMetrics(ctx); err == nil || metrics != nil || !strings.Contains(err.Error(), "calculating beta") {
		t.Fatalf("expected risk metrics to fail through beta, got metrics=%#v err=%v", metrics, err)
	}
}

func TestPortfolioVaRRejectsInvalidConfidenceBeforeDataCheck(t *testing.T) {
	analyzer := &DefaultPortfolioAnalyzer{}
	if _, err := analyzer.GetVaR(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "confidence must be between 0 and 1") {
		t.Fatalf("expected confidence validation error, got %v", err)
	}
}
