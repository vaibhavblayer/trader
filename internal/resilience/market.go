package resilience

import (
	"fmt"
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

// MarketSession represents different market sessions.
type MarketSession string

const (
	SessionPreOpen      MarketSession = "PRE_OPEN"
	SessionPreOpenMatch MarketSession = "PRE_OPEN_MATCH"
	SessionNormal       MarketSession = "NORMAL"
	SessionClosing      MarketSession = "CLOSING"
	SessionPostClose    MarketSession = "POST_CLOSE"
	SessionClosed       MarketSession = "CLOSED"
)

// MarketHoursManager provides comprehensive market hours awareness.
type MarketHoursManager struct {
	holidays map[string]bool // Date string -> is holiday
}

// NewMarketHoursManager creates a new market hours manager.
func NewMarketHoursManager() *MarketHoursManager {
	return &MarketHoursManager{
		holidays: make(map[string]bool),
	}
}

// AddHoliday adds a market holiday.
func (m *MarketHoursManager) AddHoliday(date time.Time) {
	key := date.Format("2006-01-02")
	m.holidays[key] = true
}

// IsHoliday checks if a date is a market holiday.
func (m *MarketHoursManager) IsHoliday(date time.Time) bool {
	key := date.In(IndiaLocation).Format("2006-01-02")
	return m.holidays[key]
}

// GetMarketStatus returns the current detailed market status.
func (m *MarketHoursManager) GetMarketStatus() models.MarketStatus {
	return m.GetMarketStatusAt(time.Now())
}

// GetMarketStatusAt returns the market status at a specific time.
func (m *MarketHoursManager) GetMarketStatusAt(t time.Time) models.MarketStatus {
	t = t.In(IndiaLocation)

	// Check if weekend
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return models.MarketClosed
	}

	// Check if holiday
	if m.IsHoliday(t) {
		return models.MarketClosed
	}

	hour := t.Hour()
	minute := t.Minute()
	timeMinutes := hour*60 + minute

	// Pre-open session: 9:00 - 9:08
	if timeMinutes >= 540 && timeMinutes < 548 {
		return models.MarketPreOpen
	}

	// Pre-open order matching: 9:08 - 9:15
	if timeMinutes >= 548 && timeMinutes < 555 {
		return models.MarketPreOpen
	}

	// Normal trading: 9:15 - 15:30
	if timeMinutes >= 555 && timeMinutes < 930 {
		// MIS square-off warning: 15:00 - 15:15
		if timeMinutes >= 900 && timeMinutes < 915 {
			return models.MarketMISSquareOffWarn
		}
		return models.MarketOpen
	}

	// Post-close session: 15:40 - 16:00
	if timeMinutes >= 940 && timeMinutes < 960 {
		return models.MarketClosed // Post-close is effectively closed for regular trading
	}

	return models.MarketClosed
}

// GetSession returns the current market session.
func (m *MarketHoursManager) GetSession() MarketSession {
	return m.GetSessionAt(time.Now())
}

// GetSessionAt returns the market session at a specific time.
func (m *MarketHoursManager) GetSessionAt(t time.Time) MarketSession {
	t = t.In(IndiaLocation)

	// Check if weekend or holiday
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday || m.IsHoliday(t) {
		return SessionClosed
	}

	hour := t.Hour()
	minute := t.Minute()
	timeMinutes := hour*60 + minute

	switch {
	case timeMinutes >= 540 && timeMinutes < 548:
		return SessionPreOpen
	case timeMinutes >= 548 && timeMinutes < 555:
		return SessionPreOpenMatch
	case timeMinutes >= 555 && timeMinutes < 925:
		return SessionNormal
	case timeMinutes >= 925 && timeMinutes < 930:
		return SessionClosing
	case timeMinutes >= 940 && timeMinutes < 960:
		return SessionPostClose
	default:
		return SessionClosed
	}
}

// IsMarketOpen returns true if the market is currently open for trading.
func (m *MarketHoursManager) IsMarketOpen() bool {
	status := m.GetMarketStatus()
	return status == models.MarketOpen || status == models.MarketMISSquareOffWarn
}

// IsPreMarket returns true if it's pre-market session.
func (m *MarketHoursManager) IsPreMarket() bool {
	return m.GetMarketStatus() == models.MarketPreOpen
}

// CanPlaceOrder checks if orders can be placed and returns a warning if applicable.
func (m *MarketHoursManager) CanPlaceOrder(product models.ProductType, isAMO bool) (bool, string) {
	status := m.GetMarketStatus()

	switch status {
	case models.MarketClosed:
		if isAMO {
			return true, ""
		}
		return false, "Market is closed. Use AMO (After Market Order) for next trading day."

	case models.MarketPreOpen:
		if product == models.ProductMIS {
			return false, "MIS orders cannot be placed during pre-open session."
		}
		return true, "Pre-open session: Orders will be matched at 9:08 AM."

	case models.MarketMISSquareOffWarn:
		if product == models.ProductMIS {
			return true, "⚠️ WARNING: MIS positions will be auto-squared off at 3:15 PM."
		}
		return true, ""

	case models.MarketOpen:
		return true, ""
	}

	return false, "Unknown market status"
}

// GetMISSquareOffWarning returns a warning message if MIS square-off is approaching.
func (m *MarketHoursManager) GetMISSquareOffWarning() (bool, string, time.Duration) {
	now := time.Now().In(IndiaLocation)
	squareOffTime := time.Date(now.Year(), now.Month(), now.Day(), 15, 15, 0, 0, IndiaLocation)

	if now.After(squareOffTime) {
		return false, "", 0
	}

	timeUntil := squareOffTime.Sub(now)

	// Warning thresholds
	if timeUntil <= 15*time.Minute {
		return true, fmt.Sprintf("⚠️ CRITICAL: MIS auto square-off in %v", timeUntil.Round(time.Second)), timeUntil
	}
	if timeUntil <= 30*time.Minute {
		return true, fmt.Sprintf("⚠️ WARNING: MIS auto square-off in %v", timeUntil.Round(time.Minute)), timeUntil
	}

	return false, "", timeUntil
}

// GetNextMarketOpen returns the next market opening time.
func (m *MarketHoursManager) GetNextMarketOpen() time.Time {
	now := time.Now().In(IndiaLocation)

	// Start with today at 9:15
	next := time.Date(now.Year(), now.Month(), now.Day(), 9, 15, 0, 0, IndiaLocation)

	// If already past today's open, move to tomorrow
	if now.After(next) {
		next = next.AddDate(0, 0, 1)
	}

	// Skip weekends and holidays
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday || m.IsHoliday(next) {
		next = next.AddDate(0, 0, 1)
	}

	return next
}

// GetMarketClose returns today's market close time.
func (m *MarketHoursManager) GetMarketClose() time.Time {
	now := time.Now().In(IndiaLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 15, 30, 0, 0, IndiaLocation)
}

// GetMISSquareOffTime returns today's MIS square-off time.
func (m *MarketHoursManager) GetMISSquareOffTime() time.Time {
	now := time.Now().In(IndiaLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 15, 15, 0, 0, IndiaLocation)
}

// TimeUntilMarketClose returns the duration until market close.
func (m *MarketHoursManager) TimeUntilMarketClose() time.Duration {
	return time.Until(m.GetMarketClose())
}

// TimeUntilMISSquareOff returns the duration until MIS square-off.
func (m *MarketHoursManager) TimeUntilMISSquareOff() time.Duration {
	return time.Until(m.GetMISSquareOffTime())
}

// GetTradingHoursInfo returns comprehensive trading hours information.
func (m *MarketHoursManager) GetTradingHoursInfo() TradingHoursInfo {
	now := time.Now().In(IndiaLocation)

	return TradingHoursInfo{
		CurrentTime:        now,
		Status:             m.GetMarketStatus(),
		Session:            m.GetSession(),
		IsOpen:             m.IsMarketOpen(),
		IsPreMarket:        m.IsPreMarket(),
		NextOpen:           m.GetNextMarketOpen(),
		TodayClose:         m.GetMarketClose(),
		MISSquareOff:       m.GetMISSquareOffTime(),
		TimeUntilClose:     m.TimeUntilMarketClose(),
		TimeUntilSquareOff: m.TimeUntilMISSquareOff(),
	}
}

// TradingHoursInfo contains comprehensive trading hours information.
type TradingHoursInfo struct {
	CurrentTime        time.Time
	Status             models.MarketStatus
	Session            MarketSession
	IsOpen             bool
	IsPreMarket        bool
	NextOpen           time.Time
	TodayClose         time.Time
	MISSquareOff       time.Time
	TimeUntilClose     time.Duration
	TimeUntilSquareOff time.Duration
}

// String returns a human-readable representation.
func (t TradingHoursInfo) String() string {
	if t.IsOpen {
		return fmt.Sprintf("Market OPEN | Session: %s | Closes in: %v",
			t.Session, t.TimeUntilClose.Round(time.Minute))
	}
	if t.IsPreMarket {
		return fmt.Sprintf("PRE-MARKET | Opens at: %s",
			t.NextOpen.Format("15:04"))
	}
	return fmt.Sprintf("Market CLOSED | Next open: %s",
		t.NextOpen.Format("Mon 15:04"))
}

// DefaultMarketHoursManager is a global instance with common holidays.
var DefaultMarketHoursManager = NewMarketHoursManager()

// InitializeHolidays adds known market holidays for the current year.
func InitializeHolidays(manager *MarketHoursManager, year int) {
	// 2024 NSE holidays (example - should be updated annually)
	holidays2024 := []string{
		"2024-01-26", // Republic Day
		"2024-03-08", // Maha Shivaratri
		"2024-03-25", // Holi
		"2024-03-29", // Good Friday
		"2024-04-11", // Id-Ul-Fitr
		"2024-04-14", // Dr. Ambedkar Jayanti
		"2024-04-17", // Ram Navami
		"2024-04-21", // Mahavir Jayanti
		"2024-05-01", // Maharashtra Day
		"2024-05-23", // Buddha Purnima
		"2024-06-17", // Bakri Id
		"2024-07-17", // Muharram
		"2024-08-15", // Independence Day
		"2024-10-02", // Mahatma Gandhi Jayanti
		"2024-11-01", // Diwali Laxmi Pujan
		"2024-11-15", // Guru Nanak Jayanti
		"2024-12-25", // Christmas
	}

	// 2025 NSE holidays (example)
	holidays2025 := []string{
		"2025-01-26", // Republic Day
		"2025-02-26", // Maha Shivaratri
		"2025-03-14", // Holi
		"2025-03-31", // Id-Ul-Fitr
		"2025-04-10", // Mahavir Jayanti
		"2025-04-14", // Dr. Ambedkar Jayanti
		"2025-04-18", // Good Friday
		"2025-05-01", // Maharashtra Day
		"2025-05-12", // Buddha Purnima
		"2025-06-07", // Bakri Id
		"2025-08-15", // Independence Day
		"2025-08-27", // Janmashtami
		"2025-10-02", // Mahatma Gandhi Jayanti
		"2025-10-21", // Diwali Laxmi Pujan
		"2025-11-05", // Guru Nanak Jayanti
		"2025-12-25", // Christmas
	}

	var holidays []string
	switch year {
	case 2024:
		holidays = holidays2024
	case 2025:
		holidays = holidays2025
	default:
		return
	}

	for _, dateStr := range holidays {
		date, err := time.Parse("2006-01-02", dateStr)
		if err == nil {
			manager.AddHoliday(date)
		}
	}
}
