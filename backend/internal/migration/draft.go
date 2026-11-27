package migration

import (
	"time"
	"tubes-wbd/internal/manageddb"
)

type DraftStatus string

const (
	DraftStatusDraft         DraftStatus = "draft"
	DraftStatusPendingReview DraftStatus = "pending_review"
	DraftStatusApproved      DraftStatus = "approved"
	DraftStatusQueued        DraftStatus = "queued"
	DraftStatusRunning       DraftStatus = "running"
	DraftStatusApplied       DraftStatus = "applied"
	DraftStatusFailed        DraftStatus = "failed"
)

func (s DraftStatus) open() bool {
	return s == DraftStatusDraft || s == DraftStatusFailed
}

type RunStatus string

const (
	RunStatusSuccess RunStatus = "success"
	RunStatusFailed  RunStatus = "failed"
)

type ExecutionType string

const (
	ExecutionTypeUp       ExecutionType = "up"
	ExecutionTypeRollback ExecutionType = "rollback"
	ExecutionTypeForward  ExecutionType = "forward"
)

type ExecutionStatus string

const (
	ExecutionStatusPendingReview       ExecutionStatus = "pending_review"
	ExecutionStatusRejected            ExecutionStatus = "rejected"
	ExecutionStatusApproved            ExecutionStatus = "approved"
	ExecutionStatusQueued              ExecutionStatus = "queued"
	ExecutionStatusRunning             ExecutionStatus = "running"
	ExecutionStatusSuccess             ExecutionStatus = "success"
	ExecutionStatusFailed              ExecutionStatus = "failed"
	ExecutionStatusPartial             ExecutionStatus = "partial"
	ExecutionStatusCompensationPending ExecutionStatus = "compensation_pending"
	ExecutionStatusCompensating        ExecutionStatus = "compensating"
	ExecutionStatusCompensated         ExecutionStatus = "compensated"
)

type Draft struct {
	ID                 uint             `gorm:"primaryKey" json:"id"`
	ManagedDatabaseID  uint             `gorm:"not null;index" json:"managed_database_id"`
	Name               string           `gorm:"not null" json:"name"`
	SQL                string           `gorm:"type:text;not null" json:"sql"`
	DownSQL            string           `gorm:"type:text" json:"down_sql,omitempty"`
	Batch              *BatchDefinition `gorm:"type:jsonb;serializer:json" json:"batch,omitempty"`
	Irreversible       bool             `gorm:"not null;default:false" json:"irreversible"`
	IrreversibleReason string           `gorm:"type:text" json:"irreversible_reason,omitempty"`
	Status             DraftStatus      `gorm:"not null;default:draft;index" json:"status"`
	AppliedVersion     *int64           `json:"applied_version"`
	CreatedBy          uint             `gorm:"not null" json:"created_by"`
	UpdatedBy          uint             `gorm:"not null" json:"updated_by"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`

	ManagedDatabase *manageddb.ManagedDatabase `gorm:"constraint:OnDelete:CASCADE" json:"-"`
}

func (Draft) TableName() string { return "migration_drafts" }

type Execution struct {
	ID                    uint             `gorm:"primaryKey" json:"id"`
	ManagedDatabaseID     uint             `gorm:"not null;index" json:"database_id"`
	ActiveDatabaseID      *uint            `gorm:"uniqueIndex:ux_migration_executions_active_database_id" json:"-"`
	DraftID               uint             `gorm:"not null;index" json:"draft_id"`
	Type                  ExecutionType    `gorm:"not null" json:"type"`
	Status                ExecutionStatus  `gorm:"not null;index" json:"status"`
	Name                  string           `gorm:"not null" json:"name"`
	SQL                   string           `gorm:"type:text;not null" json:"sql"`
	DownSQL               string           `gorm:"type:text" json:"down_sql,omitempty"`
	Batch                 *BatchDefinition `gorm:"type:jsonb;serializer:json" json:"batch,omitempty"`
	BatchCount            int              `gorm:"not null;default:0" json:"batch_count"`
	RowsProcessed         int64            `gorm:"not null;default:0" json:"rows_processed"`
	LastCursor            *int64           `json:"last_cursor,omitempty"`
	BatchDone             bool             `gorm:"not null;default:false" json:"batch_done"`
	Irreversible          bool             `gorm:"not null;default:false" json:"irreversible"`
	IrreversibleReason    string           `gorm:"type:text" json:"irreversible_reason,omitempty"`
	ExecutedBy            uint             `gorm:"not null" json:"executed_by"`
	TargetExecutionID     *uint            `json:"target_execution_id,omitempty"`
	Compensation          bool             `gorm:"not null;default:false" json:"compensation"`
	EncryptedTargetConfig []byte           `gorm:"type:bytea" json:"-"`
	Version               *int64           `json:"version,omitempty"`
	Attempt               int              `gorm:"not null;default:0" json:"attempt"`
	QueuedAt              *time.Time       `json:"queued_at,omitempty"`
	AvailableAt           *time.Time       `json:"available_at,omitempty"`
	StartedAt             *time.Time       `json:"started_at,omitempty"`
	FinishedAt            *time.Time       `json:"finished_at,omitempty"`
	ErrorCode             string           `json:"error_code,omitempty"`
	ErrorMessage          string           `json:"error_message,omitempty"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`

	ManagedDatabase *manageddb.ManagedDatabase `gorm:"constraint:OnDelete:RESTRICT" json:"-"`
}

func (Execution) TableName() string { return "migration_executions" }

type ExecutionAttempt struct {
	ID           uint            `gorm:"primaryKey" json:"id"`
	ExecutionID  uint            `gorm:"not null;index;uniqueIndex:ux_execution_attempt_number" json:"execution_id"`
	Number       int             `gorm:"not null;uniqueIndex:ux_execution_attempt_number" json:"number"`
	Status       ExecutionStatus `gorm:"not null;index" json:"status"`
	StartedAt    time.Time       `gorm:"not null" json:"started_at"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	Version      *int64          `json:"version,omitempty"`
}

func (ExecutionAttempt) TableName() string { return "migration_execution_attempts" }

type ExecutionOutbox struct {
	ID           uint       `gorm:"primaryKey"`
	ExecutionID  uint       `gorm:"not null;index"`
	TraceContext string     `gorm:"type:text;not null;default:''"`
	AvailableAt  time.Time  `gorm:"not null;index"`
	PublishedAt  *time.Time `gorm:"index"`
	CreatedAt    time.Time  `gorm:"not null"`
}

func (ExecutionOutbox) TableName() string { return "migration_execution_outbox" }
