package cli

import (
	"context"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/config"
)

func TestLLMForTradeDecisionsDisabledForLiveProfiles(t *testing.T) {
	app := &App{
		Config:    &config.Config{},
		LLMClient: testLLMClient{},
	}
	app.Config.Trading.Mode = "live"
	app.Config.Trading.SafetyProfile = config.SafetyProfileLiveTrading

	if got := app.llmForTradeDecisions(); got != nil {
		t.Fatalf("expected live trading to disable LLM decision authority")
	}
}

func TestLLMForTradeDecisionsEnabledForPaper(t *testing.T) {
	llm := testLLMClient{}
	app := &App{
		Config:    &config.Config{},
		LLMClient: llm,
	}
	app.Config.Trading.Mode = "paper"
	app.Config.Trading.SafetyProfile = config.SafetyProfilePaper

	if got := app.llmForTradeDecisions(); got == nil {
		t.Fatalf("expected paper trading to allow LLM simulated decision authority")
	}
}

type testLLMClient struct{}

func (testLLMClient) Complete(ctx context.Context, prompt string) (string, error) {
	return "ok", nil
}

func (testLLMClient) CompleteWithSystem(ctx context.Context, system, prompt string) (string, error) {
	return "ok", nil
}

func (testLLMClient) CompleteWithTools(ctx context.Context, systemPrompt, userPrompt string, tools []openai.Tool, executor agents.ToolExecutorInterface) (string, error) {
	return "ok", nil
}

func (testLLMClient) CompleteWithToolsVerbose(ctx context.Context, systemPrompt, userPrompt string, tools []openai.Tool, executor agents.ToolExecutorInterface) (*agents.ChainOfThought, error) {
	return &agents.ChainOfThought{Response: "ok"}, nil
}
