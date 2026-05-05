// Package cli provides the command-line interface for the trading application.
package cli

import (
	"fmt"
	"sync"
	"time"
)

// Prediction represents an AI prediction for tracking.
type Prediction struct {
	ID          string
	Symbol      string
	Action      string // BUY, SELL
	Confidence  float64
	EntryPrice  float64
	TargetPrice float64
	StopLoss    float64
	TimeWindow  time.Duration
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Reasoning   string

	// Outcome tracking
	Evaluated  bool
	ExitPrice  float64
	Outcome    string // RIGHT, WRONG, EXPIRED
	PnLPercent float64
}

// PaperTracker tracks AI predictions without executing trades.
type PaperTracker struct {
	mu          sync.RWMutex
	predictions map[string]*Prediction
	history     []*Prediction
	stats       PaperStats
}

// GetRecentHistory returns the last N evaluated predictions for context.
func (pt *PaperTracker) GetRecentHistory(n int) []*Prediction {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if len(pt.history) == 0 {
		return nil
	}

	start := 0
	if len(pt.history) > n {
		start = len(pt.history) - n
	}

	result := make([]*Prediction, len(pt.history)-start)
	copy(result, pt.history[start:])
	return result
}

// PaperStats holds prediction accuracy statistics.
type PaperStats struct {
	TotalPredictions   int
	RightPredictions   int
	WrongPredictions   int
	ExpiredPredictions int
	WinRate            float64
	AvgConfidence      float64
	AvgPnLPercent      float64
	BestPrediction     float64
	WorstPrediction    float64
}

// NewPaperTracker creates a new paper trading tracker.
func NewPaperTracker() *PaperTracker {
	return &PaperTracker{
		predictions: make(map[string]*Prediction),
		history:     make([]*Prediction, 0),
	}
}

// AddPrediction adds a new prediction to track.
func (pt *PaperTracker) AddPrediction(p *Prediction) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	p.ID = fmt.Sprintf("%s-%d", p.Symbol, time.Now().UnixNano())
	pt.predictions[p.ID] = p
	pt.stats.TotalPredictions++
	pt.stats.AvgConfidence = ((pt.stats.AvgConfidence * float64(pt.stats.TotalPredictions-1)) + p.Confidence) / float64(pt.stats.TotalPredictions)
}

// EvaluatePrediction evaluates a prediction against current price.
// Outcomes: RIGHT (target hit), WRONG (stop loss hit), EXPIRED (time ran out)
// Win rate only counts RIGHT vs WRONG for honest feedback.
func (pt *PaperTracker) EvaluatePrediction(id string, currentPrice float64) *Prediction {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	p, ok := pt.predictions[id]
	if !ok || p.Evaluated {
		return nil
	}

	p.ExitPrice = currentPrice
	p.Evaluated = true

	// Calculate P&L
	if p.Action == "BUY" {
		p.PnLPercent = ((currentPrice - p.EntryPrice) / p.EntryPrice) * 100
	} else {
		p.PnLPercent = ((p.EntryPrice - currentPrice) / p.EntryPrice) * 100
	}

	// Determine outcome
	now := time.Now()
	if now.After(p.ExpiresAt) {
		// Time expired - always mark as EXPIRED (separate from RIGHT/WRONG)
		p.Outcome = "EXPIRED"
		pt.stats.ExpiredPredictions++
	} else {
		// Check if target or stop loss hit
		if p.Action == "BUY" {
			if currentPrice >= p.TargetPrice {
				p.Outcome = "RIGHT"
				pt.stats.RightPredictions++
			} else if currentPrice <= p.StopLoss {
				p.Outcome = "WRONG"
				pt.stats.WrongPredictions++
			}
		} else {
			if currentPrice <= p.TargetPrice {
				p.Outcome = "RIGHT"
				pt.stats.RightPredictions++
			} else if currentPrice >= p.StopLoss {
				p.Outcome = "WRONG"
				pt.stats.WrongPredictions++
			}
		}
	}

	// Update stats
	if p.Outcome != "" {
		pt.history = append(pt.history, p)
		delete(pt.predictions, id)

		// Update average P&L (includes all outcomes)
		evaluated := pt.stats.RightPredictions + pt.stats.WrongPredictions + pt.stats.ExpiredPredictions
		pt.stats.AvgPnLPercent = ((pt.stats.AvgPnLPercent * float64(evaluated-1)) + p.PnLPercent) / float64(evaluated)

		// Update best/worst
		if p.PnLPercent > pt.stats.BestPrediction {
			pt.stats.BestPrediction = p.PnLPercent
		}
		if p.PnLPercent < pt.stats.WorstPrediction {
			pt.stats.WorstPrediction = p.PnLPercent
		}

		// Win rate only counts decisive outcomes (RIGHT vs WRONG)
		// EXPIRED trades don't count - they indicate signal didn't play out
		decisiveCount := pt.stats.RightPredictions + pt.stats.WrongPredictions
		if decisiveCount > 0 {
			pt.stats.WinRate = float64(pt.stats.RightPredictions) / float64(decisiveCount) * 100
		}
	}

	return p
}

// GetActivePredictions returns all active predictions.
func (pt *PaperTracker) GetActivePredictions() []*Prediction {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	result := make([]*Prediction, 0, len(pt.predictions))
	for _, p := range pt.predictions {
		result = append(result, p)
	}
	return result
}

// GetStats returns current statistics.
func (pt *PaperTracker) GetStats() PaperStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.stats
}

// CheckExpiredPredictions checks and evaluates expired predictions.
// EXPIRED is now a separate outcome - not counted in win rate calculation.
// Win rate = RIGHT / (RIGHT + WRONG), EXPIRED trades are tracked separately.
func (pt *PaperTracker) CheckExpiredPredictions(prices map[string]float64) []*Prediction {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	var expired []*Prediction
	now := time.Now()

	for id, p := range pt.predictions {
		if now.After(p.ExpiresAt) && !p.Evaluated {
			price, ok := prices[p.Symbol]
			if !ok {
				continue
			}

			p.ExitPrice = price
			p.Evaluated = true

			// Calculate P&L
			if p.Action == "BUY" {
				p.PnLPercent = ((price - p.EntryPrice) / p.EntryPrice) * 100
			} else {
				p.PnLPercent = ((p.EntryPrice - price) / p.EntryPrice) * 100
			}

			// EXPIRED is always EXPIRED - separate from RIGHT/WRONG
			// This prevents inflating win rate with lucky expired trades
			p.Outcome = "EXPIRED"
			pt.stats.ExpiredPredictions++

			pt.history = append(pt.history, p)
			delete(pt.predictions, id)
			expired = append(expired, p)

			// Update P&L stats (include expired in P&L tracking)
			evaluated := pt.stats.RightPredictions + pt.stats.WrongPredictions + pt.stats.ExpiredPredictions
			pt.stats.AvgPnLPercent = ((pt.stats.AvgPnLPercent * float64(evaluated-1)) + p.PnLPercent) / float64(evaluated)
			if p.PnLPercent > pt.stats.BestPrediction {
				pt.stats.BestPrediction = p.PnLPercent
			}
			if p.PnLPercent < pt.stats.WorstPrediction {
				pt.stats.WorstPrediction = p.PnLPercent
			}

			// Win rate only counts RIGHT vs WRONG (not EXPIRED)
			// This gives honest feedback about prediction quality
			decisiveCount := pt.stats.RightPredictions + pt.stats.WrongPredictions
			if decisiveCount > 0 {
				pt.stats.WinRate = float64(pt.stats.RightPredictions) / float64(decisiveCount) * 100
			}
		}
	}

	return expired
}
