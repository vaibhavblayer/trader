package config

import "testing"

func TestSafetyProfileDefaultsPaper(t *testing.T) {
	cfg := &Config{}
	cfg.Trading.Mode = "paper"
	if got := cfg.SafetyProfile(); got != SafetyProfilePaper {
		t.Fatalf("expected paper profile, got %s", got)
	}
	if !cfg.IsPaperMode() {
		t.Fatalf("expected paper mode")
	}
}

func TestSafetyProfileDefaultsLiveReadOnlyForLive(t *testing.T) {
	cfg := &Config{}
	cfg.Trading.Mode = "live"
	if got := cfg.SafetyProfile(); got != SafetyProfileLiveReadOnly {
		t.Fatalf("expected live-readonly profile, got %s", got)
	}
	if cfg.SafetyCapabilities().BrokerOrders {
		t.Fatalf("live-readonly must block broker orders")
	}
}

func TestLiveTradingRequiresLiveMode(t *testing.T) {
	cfg := &Config{}
	cfg.Trading.Mode = "paper"
	cfg.Trading.SafetyProfile = SafetyProfileLiveTrading
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid live-trading profile with paper mode")
	}
}

func TestLiveTradingCapabilities(t *testing.T) {
	cfg := &Config{}
	cfg.Trading.Mode = "live"
	cfg.Trading.SafetyProfile = SafetyProfileLiveTrading
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	caps := cfg.SafetyCapabilities()
	if !caps.BrokerOrders || !caps.BrokerGTT || !caps.AutoTrade {
		t.Fatalf("expected live-trading execution capabilities, got %+v", caps)
	}
	if caps.LLMOrderAuthority {
		t.Fatalf("LLM direct order authority must remain disabled for live trading")
	}
}

func TestSimulatedProfilesAllowLLMOrderAuthority(t *testing.T) {
	paper := &Config{}
	paper.Trading.Mode = "paper"
	paper.Trading.SafetyProfile = SafetyProfilePaper
	if !paper.SafetyCapabilities().LLMOrderAuthority {
		t.Fatal("expected paper profile to allow LLM-driven simulated decisions")
	}

	backtest := &Config{}
	backtest.Trading.SafetyProfile = SafetyProfileBacktest
	if !backtest.SafetyCapabilities().LLMOrderAuthority {
		t.Fatal("expected backtest profile to allow LLM-driven simulated decisions")
	}
}
