package broker

import "testing"

func TestMapTimeframeToIntervalAcceptsCommonAliases(t *testing.T) {
	tests := map[string]string{
		"1min":     "minute",
		"1minute":  "minute",
		"5m":       "5minute",
		"5minute":  "5minute",
		"15min":    "15minute",
		"15minute": "15minute",
		"30m":      "30minute",
		"30minute": "30minute",
		"1h":       "60minute",
		"1hour":    "60minute",
		"1day":     "day",
		"day":      "day",
	}
	for input, want := range tests {
		if got := mapTimeframeToInterval(input); got != want {
			t.Fatalf("mapTimeframeToInterval(%q) = %q, want %q", input, got, want)
		}
	}
}
