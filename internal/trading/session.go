// Package trading provides trading operations and utilities.
package trading

import (
	"time"
)

// MarketSession represents different market sessions.
type MarketSession string

const (
	SessionPreOpen      MarketSession = "PRE_OPEN"
	SessionPreOpenMatch MarketSession = "PRE_OPEN_MATCH"
	SessionNormal       MarketSession = "NORMAL"
	SessionClosing      MarketSession = "CLOSING"
	SessionPostClose    MarketSession = "POST_CLOSE"
	SessionClosed       MarketSession = "CLOSED"
	SessionHoliday      MarketSession = "HOLIDAY"
)

// SessionInfo represents information about current market session.
type SessionInfo struct {
	Session       MarketSession
	StartTime     time.Time
	EndTime       time.Time
	Description   string
	CanPlaceOrder bool
	OrderTypes    []string // Allowed order types in this session
}

// PreOpenOrderBook represents pre-open session order book.
type PreOpenOrderBook struct {
	Symbol          string
	IndicativePrice float64
	IndicativeQty   int64
	BuyOrders       int
	SellOrders      int
	TotalBuyQty     int64
	TotalSellQty    int64
	LastUpdated     time.Time
}

// SessionManager manages market session detection and order placement rules.
type SessionManager struct {
	location *time.Location
	holidays map[string]bool // Date string -> is holiday
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	return &SessionManager{
		location: loc,
		holidays: make(map[string]bool),
	}
}

// AddHoliday adds a market holiday.
func (m *SessionManager) AddHoliday(date time.Time) {
	key := date.Format("2006-01-02")
	m.holidays[key] = true
}

// IsHoliday checks if a date is a market holiday.
func (m *SessionManager) IsHoliday(date time.Time) bool {
	key := date.Format("2006-01-02")
	return m.holidays[key]
}

// GetCurrentSession returns the current market session.
func (m *SessionManager) GetCurrentSession() *SessionInfo {
	now := time.Now().In(m.location)
	return m.GetSessionAt(now)
}

// GetSessionAt returns the market session at a specific time.
func (m *SessionManager) GetSessionAt(t time.Time) *SessionInfo {
	t = t.In(m.location)

	// Check if weekend
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return &SessionInfo{
			Session:       SessionClosed,
			Description:   "Weekend - Market Closed",
			CanPlaceOrder: false,
		}
	}

	// Check if holiday
	if m.IsHoliday(t) {
		return &SessionInfo{
			Session:       SessionHoliday,
			Description:   "Market Holiday",
			CanPlaceOrder: false,
		}
	}

	hour := t.Hour()
	minute := t.Minute()
	timeMinutes := hour*60 + minute

	// Session timings (in minutes from midnight)
	preOpenStart := 9*60 + 0   // 9:00 AM
	preOpenEnd := 9*60 + 8     // 9:08 AM
	preOpenMatch := 9*60 + 8   // 9:08 AM
	preOpenMatchEnd := 9*60 + 15 // 9:15 AM
	normalStart := 9*60 + 15   // 9:15 AM
	closingStart := 15*60 + 20 // 3:20 PM
	normalEnd := 15*60 + 30    // 3:30 PM
	postCloseStart := 15*60 + 40 // 3:40 PM
	postCloseEnd := 16*60 + 0  // 4:00 PM

	switch {
	case timeMinutes >= preOpenStart && timeMinutes < preOpenEnd:
		return &SessionInfo{
			Session:       SessionPreOpen,
			StartTime:     timeAt(t, 9, 0),
			EndTime:       timeAt(t, 9, 8),
			Description:   "Pre-Open Session - Order Entry",
			CanPlaceOrder: true,
			OrderTypes:    []string{"LIMIT"},
		}
	case timeMinutes >= preOpenMatch && timeMinutes < preOpenMatchEnd:
		return &SessionInfo{
			Session:       SessionPreOpenMatch,
			StartTime:     timeAt(t, 9, 8),
			EndTime:       timeAt(t, 9, 15),
			Description:   "Pre-Open Session - Order Matching",
			CanPlaceOrder: false,
		}
	case timeMinutes >= normalStart && timeMinutes < closingStart:
		return &SessionInfo{
			Session:       SessionNormal,
			StartTime:     timeAt(t, 9, 15),
			EndTime:       timeAt(t, 15, 20),
			Description:   "Normal Trading Session",
			CanPlaceOrder: true,
			OrderTypes:    []string{"MARKET", "LIMIT", "SL", "SL-M"},
		}
	case timeMinutes >= closingStart && timeMinutes < normalEnd:
		return &SessionInfo{
			Session:       SessionClosing,
			StartTime:     timeAt(t, 15, 20),
			EndTime:       timeAt(t, 15, 30),
			Description:   "Closing Session",
			CanPlaceOrder: true,
			OrderTypes:    []string{"LIMIT"},
		}
	case timeMinutes >= postCloseStart && timeMinutes < postCloseEnd:
		return &SessionInfo{
			Session:       SessionPostClose,
			StartTime:     timeAt(t, 15, 40),
			EndTime:       timeAt(t, 16, 0),
			Description:   "Post-Close Session",
			CanPlaceOrder: true,
			OrderTypes:    []string{"LIMIT"},
		}
	default:
		return &SessionInfo{
			Session:       SessionClosed,
			Description:   "Market Closed",
			CanPlaceOrder: false,
		}
	}
}

// CanPlaceOrderType checks if an order type can be placed in current session.
func (m *SessionManager) CanPlaceOrderType(orderType string) bool {
	session := m.GetCurrentSession()
	if !session.CanPlaceOrder {
		return false
	}

	for _, allowed := range session.OrderTypes {
		if allowed == orderType {
			return true
		}
	}
	return false
}

// IsPreOpenSession checks if currently in pre-open session.
func (m *SessionManager) IsPreOpenSession() bool {
	session := m.GetCurrentSession()
	return session.Session == SessionPreOpen
}

// IsNormalSession checks if currently in normal trading session.
func (m *SessionManager) IsNormalSession() bool {
	session := m.GetCurrentSession()
	return session.Session == SessionNormal
}

// IsPostCloseSession checks if currently in post-close session.
func (m *SessionManager) IsPostCloseSession() bool {
	session := m.GetCurrentSession()
	return session.Session == SessionPostClose
}

// GetTimeToSessionStart returns duration until next session starts.
func (m *SessionManager) GetTimeToSessionStart(targetSession MarketSession) time.Duration {
	now := time.Now().In(m.location)
	
	var targetTime time.Time
	switch targetSession {
	case SessionPreOpen:
		targetTime = timeAt(now, 9, 0)
	case SessionNormal:
		targetTime = timeAt(now, 9, 15)
	case SessionClosing:
		targetTime = timeAt(now, 15, 20)
	case SessionPostClose:
		targetTime = timeAt(now, 15, 40)
	default:
		return 0
	}

	if targetTime.Before(now) {
		// Session already passed today, calculate for tomorrow
		targetTime = targetTime.AddDate(0, 0, 1)
	}

	return targetTime.Sub(now)
}

// GetMISSquareOffTime returns the MIS square-off time.
func (m *SessionManager) GetMISSquareOffTime() time.Time {
	now := time.Now().In(m.location)
	return timeAt(now, 15, 15) // MIS square-off at 3:15 PM
}

// GetTimeToMISSquareOff returns duration until MIS square-off.
func (m *SessionManager) GetTimeToMISSquareOff() time.Duration {
	now := time.Now().In(m.location)
	squareOff := m.GetMISSquareOffTime()
	
	if squareOff.Before(now) {
		return 0
	}
	return squareOff.Sub(now)
}

// ShouldWarnMISSquareOff checks if MIS square-off warning should be shown.
func (m *SessionManager) ShouldWarnMISSquareOff() bool {
	timeToSquareOff := m.GetTimeToMISSquareOff()
	return timeToSquareOff > 0 && timeToSquareOff <= 30*time.Minute
}

// GetNextTradingDay returns the next trading day.
func (m *SessionManager) GetNextTradingDay() time.Time {
	now := time.Now().In(m.location)
	next := now.AddDate(0, 0, 1)

	// Skip weekends
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday || m.IsHoliday(next) {
		next = next.AddDate(0, 0, 1)
	}

	return next
}

// GetAMOWindow returns the AMO (After Market Order) window.
func (m *SessionManager) GetAMOWindow() (start, end time.Time) {
	now := time.Now().In(m.location)
	
	// AMO window: 4:00 PM to 8:57 AM next day
	start = timeAt(now, 16, 0)
	end = timeAt(now.AddDate(0, 0, 1), 8, 57)
	
	return
}

// IsAMOWindow checks if currently in AMO window.
func (m *SessionManager) IsAMOWindow() bool {
	session := m.GetCurrentSession()
	if session.Session == SessionClosed {
		now := time.Now().In(m.location)
		hour := now.Hour()
		// AMO window: after 4 PM or before 8:57 AM
		return hour >= 16 || hour < 9
	}
	return false
}

// timeAt creates a time on the same day at specified hour and minute.
func timeAt(t time.Time, hour, minute int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, t.Location())
}

// GetSessionDescription returns a human-readable description of the session.
func (s MarketSession) String() string {
	switch s {
	case SessionPreOpen:
		return "Pre-Open (9:00-9:08)"
	case SessionPreOpenMatch:
		return "Pre-Open Matching (9:08-9:15)"
	case SessionNormal:
		return "Normal Trading (9:15-15:30)"
	case SessionClosing:
		return "Closing (15:20-15:30)"
	case SessionPostClose:
		return "Post-Close (15:40-16:00)"
	case SessionClosed:
		return "Closed"
	case SessionHoliday:
		return "Holiday"
	default:
		return string(s)
	}
}
