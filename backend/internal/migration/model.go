package migration

import "time"

type Migration struct {
	Name               string
	SQL                string
	DownSQL            string
	Batch              *BatchDefinition
	Irreversible       bool
	IrreversibleReason string
}

type BatchDefinition struct {
	// CursorSQL selects one positive bigint cursor value per row. It receives
	// the last committed cursor and batch size as $1 and $2.
	CursorSQL string `json:"cursor_sql"`
	BatchSize int    `json:"batch_size"`
}

type HistoryEntry struct {
	Version            int64
	ExecutionID        uint
	AttemptNumber      int
	Status             RunStatus
	ErrorCode          string
	ErrorMessage       string
	Type               ExecutionType
	TargetExecutionID  *uint
	Active             bool
	Name               string
	SQL                string
	DownSQL            string
	IrreversibleReason string
	ExecutedBy         uint
	ExecutedAt         time.Time
	Duration           time.Duration
}

type HistorySummary struct {
	Version           int64
	ExecutionID       *uint
	AttemptNumber     int
	Status            RunStatus
	ErrorCode         string
	ErrorMessage      string
	Type              ExecutionType
	TargetExecutionID *uint
	Active            bool
	Name              string
	ExecutedBy        uint
	ExecutedAt        time.Time
	Duration          time.Duration
}

type HistoryPage struct {
	Entries  []HistorySummary
	Page     int
	PageSize int
	HasNext  bool
}
