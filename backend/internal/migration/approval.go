package migration

import (
	"time"
	"tubes-wbd/internal/access"
)

type ApprovalDecision string

const (
	ApprovalDecisionPending  ApprovalDecision = "pending"
	ApprovalDecisionApproved ApprovalDecision = "approved"
	ApprovalDecisionRejected ApprovalDecision = "rejected"
)

type ExecutionApproval struct {
	ID          uint             `gorm:"primaryKey" json:"id"`
	ExecutionID uint             `gorm:"not null;uniqueIndex:ux_execution_approval_group" json:"execution_id"`
	GroupID     uint             `gorm:"not null;uniqueIndex:ux_execution_approval_group;index" json:"group_id"`
	GroupName   string           `gorm:"not null" json:"group_name"`
	Decision    ApprovalDecision `gorm:"type:text;not null;default:pending;index;check:chk_execution_approval_decision,decision IN ('pending', 'approved', 'rejected')" json:"decision"`
	DecidedBy   *uint            `json:"decided_by,omitempty"`
	DecidedAt   *time.Time       `gorm:"check:chk_execution_approval_decided,(decision = 'pending') = (decided_by IS NULL AND decided_at IS NULL)" json:"decided_at,omitempty"`
	Note        string           `gorm:"type:text;not null;default:''" json:"note,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`

	Execution *Execution    `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	Group     *access.Group `gorm:"constraint:OnDelete:RESTRICT" json:"-"`
}

func (ExecutionApproval) TableName() string { return "migration_execution_approvals" }
