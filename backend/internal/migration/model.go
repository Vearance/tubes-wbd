package migration

import "time"

type Migration struct {
	Name               string
	SQL                string
	DownSQL            string
	Irreversible       bool
	IrreversibleReason string
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
