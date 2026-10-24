package migration

import "time"

type Migration struct {
	Name string
	SQL  string
}

type HistoryEntry struct {
	Version    int64
	Name       string
	SQL        string
	ExecutedBy uint
	ExecutedAt time.Time
	Duration   time.Duration
}

type HistorySummary struct {
	Version    int64
	Name       string
	ExecutedBy uint
	ExecutedAt time.Time
	Duration   time.Duration
}

type HistoryPage struct {
	Entries  []HistorySummary
	Page     int
	PageSize int
	HasNext  bool
}