package models

import "time"

// Alert represents a price alert.
type Alert struct {
	ID        string
	Symbol    string
	Condition string // above, below, percent_change
	Price     float64
	Triggered bool
	CreatedAt time.Time
	TriggeredAt *time.Time
}

// CorporateEvent represents a corporate event.
type CorporateEvent struct {
	ID          string
	Symbol      string
	EventType   string // dividend, bonus, split, rights, agm, results
	Date        time.Time
	Description string
	Details     map[string]interface{}
	CreatedAt   time.Time
}
