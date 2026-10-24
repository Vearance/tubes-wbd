package migration

import (
	"time"
	"tubes-wbd/backend/manageddb"
)

type DraftStatus string

const (
	DraftStatusDraft   DraftStatus = "draft"
	DraftStatusRunning DraftStatus = "running"
	DraftStatusApplied DraftStatus = "applied"
	DraftStatusFailed  DraftStatus = "failed"
)

func (s DraftStatus) open() bool {
	return s == DraftStatusDraft || s == DraftStatusFailed
}

type RunStatus string

const (
	RunStatusSuccess RunStatus = "success"
	RunStatusFailed  RunStatus = "failed"
)

type Draft struct {
	ID                uint        `gorm:"primaryKey" json:"id"`
	ManagedDatabaseID uint        `gorm:"not null;index" json:"managed_database_id"`
	Name              string      `gorm:"not null" json:"name"`
	SQL               string      `gorm:"type:text;not null" json:"sql"`
	Status            DraftStatus `gorm:"not null;default:draft;index" json:"status"`
	AppliedVersion    *int64      `json:"applied_version"`
	CreatedBy         uint        `gorm:"not null" json:"created_by"`
	UpdatedBy         uint        `gorm:"not null" json:"updated_by"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`

	ManagedDatabase *manageddb.ManagedDatabase `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}

func (Draft) TableName() string { return "migration_drafts" }
type Run struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	DraftID           uint      `gorm:"not null;index" json:"draft_id"`
	ManagedDatabaseID uint      `gorm:"not null;index" json:"managed_database_id"`
	Status            RunStatus `gorm:"not null" json:"status"`
	Version           *int64    `json:"version"`
	SQL               string    `gorm:"type:text;not null" json:"sql"`
	ExecutedBy        uint      `gorm:"not null" json:"executed_by"`
	StartedAt         time.Time `gorm:"not null" json:"started_at"`
	FinishedAt        time.Time `gorm:"not null" json:"finished_at"`
	ErrorCode         string    `json:"error_code,omitempty"`
	ErrorMessage      string    `json:"error_message,omitempty"`

	Draft *Draft `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}

func (Run) TableName() string { return "migration_runs" }