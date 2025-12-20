package utils

import (
	"time"

	"zerodha-trader/internal/models"
)

// IndiaLocation is the timezone for Indian markets.
var IndiaLocation *time.Location

func init() {
	var err error
	IndiaLocation, err = time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// Fallback to UTC+5:30
		IndiaLocation = time.FixedZone("IST", 5*60*60+30*60)
	}
}

// GetMarketStatus returns the current market status.
func GetMarketStatus() models.MarketStatus {
	now := time.Now().In(IndiaLocation)
	
	// Check if weekend
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		return models.MarketClosed
	}

	hour := now.Hour()
	minute := now.Minute()
	timeMinutes := hour*60 + minute

	// Pre-open: 9:00 - 9:15
	if timeMinutes >= 540 && timeMinutes < 555 {
		return models.MarketPreOpen
	}

	// Market open: 9:15 - 15:30
	if timeMinutes >= 555 && timeMinutes < 930 {
		// MIS square-off warning: 15:00 - 15:15
		if timeMinutes >= 900 && timeMinutes < 915 {
			return models.MarketMISSquareOffWarn
		}
		return models.MarketOpen
	}

	return models.MarketClosed
}

// IsMarketOpen returns true if the market is currently open.
func IsMarketOpen() bool {
	status := GetMarketStatus()
	return status == models.MarketOpen || status == models.MarketMISSquareOffWarn
}

// IsPreMarket returns true if it's pre-market session.
func IsPreMarket() bool {
	return GetMarketStatus() == models.MarketPreOpen
}

// GetNextMarketOpen returns the next market opening time.
func GetNextMarketOpen() time.Time {
	now := time.Now().In(IndiaLocation)
	
	// Start with today at 9:15
	next := time.Date(now.Year(), now.Month(), now.Day(), 9, 15, 0, 0, IndiaLocation)
	
	// If already past today's open, move to tomorrow
	if now.After(next) {
		next = next.AddDate(0, 0, 1)
	}
	
	// Skip weekends
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		next = next.AddDate(0, 0, 1)
	}
	
	return next
}

// GetMarketClose returns today's market close time.
func GetMarketClose() time.Time {
	now := time.Now().In(IndiaLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 15, 30, 0, 0, IndiaLocation)
}

// GetMISSquareOffTime returns today's MIS square-off time.
func GetMISSquareOffTime() time.Time {
	now := time.Now().In(IndiaLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 15, 15, 0, 0, IndiaLocation)
}

// TimeUntilMarketClose returns the duration until market close.
func TimeUntilMarketClose() time.Duration {
	return time.Until(GetMarketClose())
}

// TimeUntilMISSquareOff returns the duration until MIS square-off.
func TimeUntilMISSquareOff() time.Duration {
	return time.Until(GetMISSquareOffTime())
}
