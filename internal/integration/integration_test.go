// Package integration provides end-to-end integration tests for the trading system.
// Requirements: 22.7, 22.11
package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/stream"
	"zerodha-trader/internal/trading"
)

// TestEndToEndWorkflow tests the complete workflow from data reception to trade decision.
// This validates Requirements 22.7 (modular architecture) and 22.11 (agent decision logging).
func TestEndToEndWorkflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup paper broker
	paperBroker := broker.NewPaperBroker(broker.PaperBrokerConfig{
		InitialBalance: 1000000, // 10 lakhs
	})

	// Setup stream hub
	hub := stream.NewHub()
	if err := hub.Start(ctx); err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer hub.Stop()

	// Setup agents
	agentWeights := map[string]float64{
		"technical": 0.35,
		"research":  0.25,
		"news":      0.15,
		"risk":      0.25,
	}

	technicalAgent := agents.NewTechnicalAgent(nil, 0.35)
	riskAgent := agents.NewRiskAgent(nil, 0.25)
	traderAgent := agents.NewTraderAgent(nil, agentWeights, 1.0)

	agentList := []agents.Agent{technicalAgent}

	// Setup orchestrator
	agentConfig := &config.AgentConfig{
		AutonomousMode:       "FULL_AUTO",
		AutoExecuteThreshold: 70,
		MaxDailyTrades:       10,
		MaxDailyLoss:         5000,
		CooldownMinutes:      0,
		ConsecutiveLossLimit: 3,
	}

	orchestrator := agents.NewOrchestrator(
		agentList,
		traderAgent,
		riskAgent,
		agentConfig,
		nil, // No store for this test
		nil, // No notifier for this test
	)

	if err := orchestrator.Start(ctx); err != nil {
		t.Fatalf("Failed to start orchestrator: %v", err)
	}
	defer orchestrator.Stop()

	// Test 1: Verify paper broker is authenticated
	if !paperBroker.IsAuthenticated() {
		t.Error("Paper broker should always be authenticated")
	}

	// Test 2: Place a paper order
	order := &models.Order{
		Symbol:   "RELIANCE",
		Exchange: models.NSE,
		Side:     models.OrderSideBuy,
		Type:     models.OrderTypeMarket,
		Product:  models.ProductMIS,
		Quantity: 10,
	}

	// Update price cache first
	paperBroker.UpdatePrice("RELIANCE", 2500.0)

	result, err := paperBroker.PlaceOrder(ctx, order)
	if err != nil {
		t.Fatalf("Failed to place paper order: %v", err)
	}

	if result.OrderID == "" {
		t.Error("Order ID should not be empty")
	}

	// Test 3: Verify position was created
	positions, err := paperBroker.GetPositions(ctx)
	if err != nil {
		t.Fatalf("Failed to get positions: %v", err)
	}

	if len(positions) == 0 {
		t.Error("Expected at least one position after order")
	}

	// Test 4: Verify balance was updated
	balance, err := paperBroker.GetBalance(ctx)
	if err != nil {
		t.Fatalf("Failed to get balance: %v", err)
	}

	if balance.AvailableCash >= 1000000 {
		t.Error("Available cash should have decreased after buy order")
	}

	// Test 5: Process symbol through orchestrator
	req := agents.AnalysisRequest{
		Symbol:       "RELIANCE",
		CurrentPrice: 2500.0,
		SignalScore: &analysis.SignalScore{
			Score:          50,
			Recommendation: analysis.Buy,
		},
		Portfolio: &agents.PortfolioState{
			TotalValue:    1000000,
			AvailableCash: 500000,
		},
	}

	decision, err := orchestrator.ProcessSymbol(ctx, req)
	if err != nil {
		t.Fatalf("Failed to process symbol: %v", err)
	}

	if decision == nil {
		t.Fatal("Decision should not be nil")
	}

	// Verify decision has required fields
	if decision.Symbol != "RELIANCE" {
		t.Errorf("Expected symbol RELIANCE, got %s", decision.Symbol)
	}

	if decision.Confidence < 0 || decision.Confidence > 100 {
		t.Errorf("Confidence should be in [0, 100], got %f", decision.Confidence)
	}

	t.Logf("End-to-end workflow test passed: Decision=%s, Confidence=%.2f", decision.Action, decision.Confidence)
}

// TestPaperTradingSimulation tests the paper trading simulation functionality.
// This validates Requirements 10.1-10.5 (paper trading mode).
func TestPaperTradingSimulation(t *testing.T) {
	ctx := context.Background()

	// Create paper broker with initial balance
	initialBalance := 500000.0
	paperBroker := broker.NewPaperBroker(broker.PaperBrokerConfig{
		InitialBalance: initialBalance,
	})

	// Test 1: Initial state
	balance, err := paperBroker.GetBalance(ctx)
	if err != nil {
		t.Fatalf("Failed to get initial balance: %v", err)
	}

	if balance.AvailableCash != initialBalance {
		t.Errorf("Expected initial balance %.2f, got %.2f", initialBalance, balance.AvailableCash)
	}

	// Test 2: Place buy order
	paperBroker.UpdatePrice("TCS", 3500.0)

	buyOrder := &models.Order{
		Symbol:   "TCS",
		Exchange: models.NSE,
		Side:     models.OrderSideBuy,
		Type:     models.OrderTypeMarket,
		Product:  models.ProductMIS,
		Quantity: 10,
	}

	buyResult, err := paperBroker.PlaceOrder(ctx, buyOrder)
	if err != nil {
		t.Fatalf("Failed to place buy order: %v", err)
	}

	if buyResult.Status != "COMPLETE" {
		t.Errorf("Expected order status COMPLETE, got %s", buyResult.Status)
	}

	// Test 3: Verify position
	positions, err := paperBroker.GetPositions(ctx)
	if err != nil {
		t.Fatalf("Failed to get positions: %v", err)
	}

	var tcsPosition *models.Position
	for i := range positions {
		if positions[i].Symbol == "TCS" {
			tcsPosition = &positions[i]
			break
		}
	}

	if tcsPosition == nil {
		t.Fatal("Expected TCS position to exist")
	}

	if tcsPosition.Quantity != 10 {
		t.Errorf("Expected quantity 10, got %d", tcsPosition.Quantity)
	}

	// Test 4: Update price and check P&L
	newPrice := 3600.0
	paperBroker.UpdatePrice("TCS", newPrice)

	positions, _ = paperBroker.GetPositions(ctx)
	for i := range positions {
		if positions[i].Symbol == "TCS" {
			tcsPosition = &positions[i]
			break
		}
	}

	expectedPnL := (newPrice - 3500.0) * 10
	if tcsPosition.PnL != expectedPnL {
		t.Errorf("Expected P&L %.2f, got %.2f", expectedPnL, tcsPosition.PnL)
	}

	// Test 5: Place sell order to close position
	sellOrder := &models.Order{
		Symbol:   "TCS",
		Exchange: models.NSE,
		Side:     models.OrderSideSell,
		Type:     models.OrderTypeMarket,
		Product:  models.ProductMIS,
		Quantity: 10,
	}

	sellResult, err := paperBroker.PlaceOrder(ctx, sellOrder)
	if err != nil {
		t.Fatalf("Failed to place sell order: %v", err)
	}

	if sellResult.Status != "COMPLETE" {
		t.Errorf("Expected order status COMPLETE, got %s", sellResult.Status)
	}

	// Test 6: Verify position is closed
	positions, _ = paperBroker.GetPositions(ctx)
	for _, pos := range positions {
		if pos.Symbol == "TCS" && pos.Quantity != 0 {
			t.Error("Expected TCS position to be closed")
		}
	}

	// Test 7: Verify balance reflects profit
	finalBalance, _ := paperBroker.GetBalance(ctx)
	expectedBalance := initialBalance - (3500.0 * 10) + (3600.0 * 10)
	if finalBalance.AvailableCash != expectedBalance {
		t.Errorf("Expected final balance %.2f, got %.2f", expectedBalance, finalBalance.AvailableCash)
	}

	t.Logf("Paper trading simulation test passed: Initial=%.2f, Final=%.2f, Profit=%.2f",
		initialBalance, finalBalance.AvailableCash, finalBalance.AvailableCash-initialBalance)
}

// TestAgentCoordination tests that multiple agents coordinate correctly.
// This validates Requirements 11.1, 11.7-11.12, 14.1-14.6.
func TestAgentCoordination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create multiple agents
	agentWeights := map[string]float64{
		"technical": 0.35,
		"research":  0.25,
		"news":      0.15,
		"risk":      0.25,
	}

	technicalAgent := agents.NewTechnicalAgent(nil, 0.35)
	researchAgent := agents.NewResearchAgent(nil, nil, 0.25)
	newsAgent := agents.NewNewsAgent(nil, nil, 0.15)
	riskAgent := agents.NewRiskAgent(nil, 0.25)
	traderAgent := agents.NewTraderAgent(nil, agentWeights, 1.0)

	agentList := []agents.Agent{
		technicalAgent,
		researchAgent,
		newsAgent,
	}

	// Setup orchestrator
	agentConfig := &config.AgentConfig{
		AutonomousMode:       "FULL_AUTO",
		AutoExecuteThreshold: 70,
		MaxDailyTrades:       10,
		MaxDailyLoss:         5000,
		CooldownMinutes:      0,
		ConsecutiveLossLimit: 3,
	}

	orchestrator := agents.NewOrchestrator(
		agentList,
		traderAgent,
		riskAgent,
		agentConfig,
		nil,
		nil,
	)

	if err := orchestrator.Start(ctx); err != nil {
		t.Fatalf("Failed to start orchestrator: %v", err)
	}
	defer orchestrator.Stop()

	// Test 1: Verify all agents are registered
	registeredAgents := orchestrator.GetAgents()
	if len(registeredAgents) != 3 {
		t.Errorf("Expected 3 agents, got %d", len(registeredAgents))
	}

	// Test 2: Verify orchestrator status
	status := orchestrator.GetStatus()
	if !status.Running {
		t.Error("Orchestrator should be running")
	}

	if status.Paused {
		t.Error("Orchestrator should not be paused")
	}

	// Test 3: Process symbol and verify all agents contribute
	req := agents.AnalysisRequest{
		Symbol:       "INFY",
		CurrentPrice: 1500.0,
		SignalScore: &analysis.SignalScore{
			Score:          60,
			Recommendation: analysis.Buy,
		},
		Portfolio: &agents.PortfolioState{
			TotalValue:    1000000,
			AvailableCash: 500000,
		},
		MarketState: &agents.MarketState{
			NiftyLevel:  18000,
			VIXLevel:    15,
			MarketTrend: "BULLISH",
		},
	}

	decision, err := orchestrator.ProcessSymbol(ctx, req)
	if err != nil {
		t.Fatalf("Failed to process symbol: %v", err)
	}

	// Test 4: Verify consensus was calculated
	if decision.Consensus == nil {
		t.Error("Decision should have consensus details")
	} else {
		if decision.Consensus.TotalAgents == 0 {
			t.Error("Consensus should have at least one agent")
		}
		if decision.Consensus.WeightedScore < 0 || decision.Consensus.WeightedScore > 100 {
			t.Errorf("Weighted score should be in [0, 100], got %f", decision.Consensus.WeightedScore)
		}
	}

	// Test 5: Verify risk check was performed
	if decision.RiskCheck == nil {
		t.Error("Decision should have risk check result")
	}

	// Test 6: Test pause/resume
	if err := orchestrator.Pause(); err != nil {
		t.Fatalf("Failed to pause orchestrator: %v", err)
	}

	status = orchestrator.GetStatus()
	if !status.Paused {
		t.Error("Orchestrator should be paused")
	}

	// Processing should fail when paused
	_, err = orchestrator.ProcessSymbol(ctx, req)
	if err == nil {
		t.Error("Processing should fail when orchestrator is paused")
	}

	if err := orchestrator.Resume(); err != nil {
		t.Fatalf("Failed to resume orchestrator: %v", err)
	}

	status = orchestrator.GetStatus()
	if status.Paused {
		t.Error("Orchestrator should not be paused after resume")
	}

	t.Logf("Agent coordination test passed: Agents=%d, Consensus=%.2f",
		len(registeredAgents), decision.Consensus.WeightedScore)
}

// TestStreamHubIntegration tests the stream hub with multiple consumers.
// This validates Requirements 2.2, 2.6, 18.1, 18.5.
func TestStreamHubIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hub := stream.NewHub()
	if err := hub.Start(ctx); err != nil {
		t.Fatalf("Failed to start hub: %v", err)
	}
	defer hub.Stop()

	// Subscribe multiple consumers to same symbol
	symbol := "HDFC"
	numConsumers := 5
	channels := make([]<-chan models.Tick, numConsumers)

	for i := 0; i < numConsumers; i++ {
		channels[i] = hub.Subscribe(symbol)
	}

	// Verify subscriber count
	if hub.GetSubscriberCount(symbol) != numConsumers {
		t.Errorf("Expected %d subscribers, got %d", numConsumers, hub.GetSubscriberCount(symbol))
	}

	// Publish ticks and verify all consumers receive them
	numTicks := 10
	var wg sync.WaitGroup
	receivedCounts := make([]int, numConsumers)

	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func(idx int, ch <-chan models.Tick) {
			defer wg.Done()
			timeout := time.After(5 * time.Second)
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
					receivedCounts[idx]++
					if receivedCounts[idx] >= numTicks {
						return
					}
				case <-timeout:
					return
				}
			}
		}(i, channels[i])
	}

	// Give consumers time to start
	time.Sleep(50 * time.Millisecond)

	// Publish ticks
	for i := 0; i < numTicks; i++ {
		tick := models.Tick{
			Symbol:    symbol,
			LTP:       1500.0 + float64(i),
			Timestamp: time.Now(),
		}
		hub.Publish(tick)
		time.Sleep(10 * time.Millisecond)
	}

	wg.Wait()

	// Verify all consumers received ticks
	for i, count := range receivedCounts {
		if count < numTicks/2 { // Allow some tolerance
			t.Errorf("Consumer %d received only %d ticks, expected at least %d", i, count, numTicks/2)
		}
	}

	// Test metrics
	metrics := hub.GetMetrics()
	if metrics.TicksReceived == 0 {
		t.Error("Expected some ticks to be received")
	}

	t.Logf("Stream hub integration test passed: Consumers=%d, TicksReceived=%d",
		numConsumers, metrics.TicksReceived)
}

// TestExecutionPipelineIntegration tests the decision pipeline with execution logic.
// This validates Requirements 13.1-13.10, 26.1-26.12.
func TestExecutionPipelineIntegration(t *testing.T) {
	ctx := context.Background()

	// Setup agents
	agentWeights := map[string]float64{
		"technical": 0.35,
		"research":  0.25,
		"news":      0.15,
		"risk":      0.25,
	}

	technicalAgent := agents.NewTechnicalAgent(nil, 0.35)
	riskAgent := agents.NewRiskAgent(nil, 0.25)
	traderAgent := agents.NewTraderAgent(nil, agentWeights, 1.0)

	agentList := []agents.Agent{technicalAgent}

	agentConfig := &config.AgentConfig{
		AutonomousMode:       "FULL_AUTO",
		AutoExecuteThreshold: 70,
		MaxDailyTrades:       10,
		MaxDailyLoss:         5000,
		CooldownMinutes:      0,
		ConsecutiveLossLimit: 3,
	}

	riskConfig := &config.RiskConfig{
		MaxPositionPercent:     5,
		MaxSectorExposure:      20,
		MaxConcurrentPositions: 5,
		MinRiskReward:          1.5,
	}

	orchestrator := agents.NewOrchestrator(
		agentList,
		traderAgent,
		riskAgent,
		agentConfig,
		nil,
		nil,
	)

	orchestrator.Start(ctx)
	defer orchestrator.Stop()

	pipeline := trading.NewDecisionPipeline(
		orchestrator,
		riskAgent,
		agentConfig,
		riskConfig,
		nil,
		nil,
	)

	// Test 1: Process with high confidence signal
	input := trading.PipelineInput{
		Symbol:       "SBIN",
		CurrentPrice: 600.0,
		SignalScore: &analysis.SignalScore{
			Score:          80,
			Recommendation: analysis.StrongBuy,
		},
		Portfolio: &agents.PortfolioState{
			TotalValue:    1000000,
			AvailableCash: 500000,
		},
		MarketState: &agents.MarketState{
			NiftyLevel:  18000,
			VIXLevel:    12,
			MarketTrend: "BULLISH",
		},
	}

	output, err := pipeline.Process(ctx, input)
	if err != nil {
		t.Fatalf("Failed to process pipeline: %v", err)
	}

	if output.Decision == nil {
		t.Fatal("Decision should not be nil")
	}

	// Test 2: Verify risk assessment
	if output.RiskAssessment == nil {
		t.Error("Risk assessment should not be nil")
	}

	// Test 3: Test daily stats tracking
	pipeline.RecordTradeOutcome("test-1", models.OutcomeWin, 1000)
	stats := pipeline.GetDailyStats()

	if stats.Trades != 1 {
		t.Errorf("Expected 1 trade, got %d", stats.Trades)
	}

	// Test 4: Test accuracy stats
	accuracyStats := pipeline.GetAccuracyStats()
	if accuracyStats.TotalDecisions == 0 {
		t.Error("Expected at least one decision tracked")
	}

	// Test 5: Test can trade check
	canTrade, reason := pipeline.CanTrade()
	if !canTrade {
		t.Errorf("Should be able to trade: %s", reason)
	}

	t.Logf("Execution pipeline integration test passed: Decision=%s, Confidence=%.2f",
		output.Decision.Action, output.Decision.Confidence)
}

// TestExecutionCheckerIntegration tests the execution checker with various scenarios.
func TestExecutionCheckerIntegration(t *testing.T) {
	agentConfig := &config.AgentConfig{
		AutonomousMode:       "FULL_AUTO",
		AutoExecuteThreshold: 75,
		MaxDailyTrades:       5,
		MaxDailyLoss:         2000,
		CooldownMinutes:      5,
		ConsecutiveLossLimit: 2,
	}

	checker := trading.NewExecutionChecker(agentConfig)

	// Test 1: Should execute - all conditions met
	decision1 := &models.Decision{
		ID:         "TEST-001",
		Symbol:     "RELIANCE",
		Action:     "BUY",
		Confidence: 85,
		RiskCheck: &models.RiskCheckResult{
			Approved: true,
		},
		Consensus: &models.ConsensusDetails{
			TotalAgents:    4,
			AgreeingAgents: 4,
		},
	}

	state1 := trading.ExecutionState{
		DailyTrades:       0,
		DailyLoss:         0,
		LastTradeAt:       time.Time{},
		ConsecutiveLosses: 0,
	}

	result1 := checker.CheckExecution(decision1, state1)
	if !result1.ShouldExecute {
		t.Errorf("Should execute when all conditions met: %s", result1.BlockReason)
	}

	// Test 2: Should not execute - confidence below threshold
	decision2 := &models.Decision{
		ID:         "TEST-002",
		Symbol:     "TCS",
		Action:     "BUY",
		Confidence: 60, // Below 75 threshold
		RiskCheck: &models.RiskCheckResult{
			Approved: true,
		},
	}

	result2 := checker.CheckExecution(decision2, state1)
	if result2.ShouldExecute {
		t.Error("Should not execute when confidence below threshold")
	}

	// Test 3: Should not execute - risk not approved
	decision3 := &models.Decision{
		ID:         "TEST-003",
		Symbol:     "INFY",
		Action:     "BUY",
		Confidence: 90,
		RiskCheck: &models.RiskCheckResult{
			Approved:   false,
			Violations: []string{"position size exceeded"},
		},
	}

	result3 := checker.CheckExecution(decision3, state1)
	if result3.ShouldExecute {
		t.Error("Should not execute when risk not approved")
	}

	// Test 4: Should not execute - daily trade limit reached
	state4 := trading.ExecutionState{
		DailyTrades: 5, // At limit
	}

	result4 := checker.CheckExecution(decision1, state4)
	if result4.ShouldExecute {
		t.Error("Should not execute when daily trade limit reached")
	}

	// Test 5: Should not execute - cooldown period active
	state5 := trading.ExecutionState{
		LastTradeAt: time.Now().Add(-2 * time.Minute), // 2 minutes ago, cooldown is 5
	}

	result5 := checker.CheckExecution(decision1, state5)
	if result5.ShouldExecute {
		t.Error("Should not execute during cooldown period")
	}

	t.Log("Execution checker integration test passed")
}

// TestConcurrentAgentProcessing tests that agents can process concurrently.
func TestConcurrentAgentProcessing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	agentWeights := map[string]float64{
		"technical": 0.35,
		"research":  0.25,
		"news":      0.15,
		"risk":      0.25,
	}

	technicalAgent := agents.NewTechnicalAgent(nil, 0.35)
	riskAgent := agents.NewRiskAgent(nil, 0.25)
	traderAgent := agents.NewTraderAgent(nil, agentWeights, 1.0)

	agentList := []agents.Agent{technicalAgent}

	agentConfig := &config.AgentConfig{
		AutonomousMode:       "FULL_AUTO",
		AutoExecuteThreshold: 70,
		MaxDailyTrades:       100,
		MaxDailyLoss:         50000,
		CooldownMinutes:      0,
		ConsecutiveLossLimit: 10,
	}

	orchestrator := agents.NewOrchestrator(
		agentList,
		traderAgent,
		riskAgent,
		agentConfig,
		nil,
		nil,
	)

	orchestrator.Start(ctx)
	defer orchestrator.Stop()

	// Process multiple symbols concurrently
	symbols := []string{"RELIANCE", "TCS", "INFY", "HDFC", "ICICI"}
	var wg sync.WaitGroup
	results := make(chan *models.Decision, len(symbols))
	errors := make(chan error, len(symbols))

	for _, symbol := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()

			req := agents.AnalysisRequest{
				Symbol:       sym,
				CurrentPrice: 1000.0,
				SignalScore: &analysis.SignalScore{
					Score:          50,
					Recommendation: analysis.Buy,
				},
				Portfolio: &agents.PortfolioState{
					TotalValue:    1000000,
					AvailableCash: 500000,
				},
			}

			decision, err := orchestrator.ProcessSymbol(ctx, req)
			if err != nil {
				errors <- err
				return
			}
			results <- decision
		}(symbol)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Error processing symbol: %v", err)
	}

	// Verify all symbols were processed
	processedCount := 0
	for decision := range results {
		if decision != nil {
			processedCount++
		}
	}

	if processedCount != len(symbols) {
		t.Errorf("Expected %d decisions, got %d", len(symbols), processedCount)
	}

	t.Logf("Concurrent agent processing test passed: Processed %d symbols", processedCount)
}
