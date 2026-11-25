package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"tubes-wbd/internal/access"
)

const maxApprovalNoteLen = 2000

var (
	ErrNoApprovers               = errors.New("migration: no group has access to this database, so nobody can approve the change")
	ErrGroupLeaderMissing        = errors.New("migration: every group with access to this database needs a leader before changes can be submitted")
	ErrNotApprover               = errors.New("migration: you do not lead any group whose approval is still pending")
	ErrExecutionNotPendingReview = errors.New("migration: execution is not awaiting review")
	ErrApprovalsOutdated         = errors.New("migration: another group gained access to this database after submission and must approve before the execution can be queued")
)

type ApprovalView struct {
	GroupID           uint
	GroupName         string
	LeaderUserID      *uint
	LeaderUsername    *string
	Decision          ApprovalDecision
	DecidedBy         *uint
	DecidedByUsername *string
	DecidedAt         *time.Time
	Note              string
}

type ApprovalSummary struct {
	ExecutionID     uint
	ExecutionStatus ExecutionStatus
	CanDecide       bool
	Approvals       []ApprovalView
}

type approvalRepository interface {
	executionRepository
	DecideExecution(ctx context.Context, executionID, userID uint, decision ApprovalDecision, note string) error
	ListExecutionApprovals(ctx context.Context, executionID uint) ([]ApprovalView, error)
	ListPendingApprovals(ctx context.Context, userID uint) ([]Execution, error)
}

var _ approvalRepository = (*GormRepository)(nil)

func (s *MigrationService) approvalRepo() (approvalRepository, error) {
	repo, ok := s.repo.(approvalRepository)
	if !ok {
		return nil, errors.New("migration: repository does not support approvals")
	}
	return repo, nil
}

func (s *MigrationService) ApproveExecution(ctx context.Context, userID, executionID uint, note string) (*ApprovalSummary, error) {
	return s.decide(ctx, userID, executionID, ApprovalDecisionApproved, note)
}

func (s *MigrationService) RejectExecution(ctx context.Context, userID, executionID uint, note string) (*ApprovalSummary, error) {
	if strings.TrimSpace(note) == "" {
		return nil, fmt.Errorf("%w: a reason is required to reject a change", ErrInvalidInput)
	}
	return s.decide(ctx, userID, executionID, ApprovalDecisionRejected, note)
}

func (s *MigrationService) decide(ctx context.Context, userID, executionID uint, decision ApprovalDecision, note string) (*ApprovalSummary, error) {
	repo, err := s.approvalRepo()
	if err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxApprovalNoteLen {
		return nil, fmt.Errorf("%w: note must be at most %d characters", ErrInvalidInput, maxApprovalNoteLen)
	}
	execution, err := repo.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireView(ctx, userID, execution.ManagedDatabaseID); err != nil {
		return nil, err
	}
	if err := repo.DecideExecution(ctx, executionID, userID, decision, note); err != nil {
		return nil, err
	}
	return approvalSummary(ctx, repo, userID, executionID)
}

func (s *MigrationService) ExecutionApprovals(ctx context.Context, userID, executionID uint) (*ApprovalSummary, error) {
	repo, err := s.approvalRepo()
	if err != nil {
		return nil, err
	}
	execution, err := repo.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireView(ctx, userID, execution.ManagedDatabaseID); err != nil {
		return nil, err
	}
	return approvalSummary(ctx, repo, userID, executionID)
}

func (s *MigrationService) EnqueueExecution(ctx context.Context, userID, executionID uint) (*Execution, error) {
	repo, err := s.approvalRepo()
	if err != nil {
		return nil, err
	}
	execution, err := repo.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireEdit(ctx, userID, execution.ManagedDatabaseID); err != nil {
		return nil, err
	}
	if err := repo.EnqueueApprovedExecution(ctx, executionID); err != nil {
		return nil, err
	}
	return repo.GetExecution(ctx, executionID)
}

func (s *MigrationService) PendingApprovals(ctx context.Context, userID uint) ([]Execution, error) {
	repo, err := s.approvalRepo()
	if err != nil {
		return nil, err
	}
	executions, err := repo.ListPendingApprovals(ctx, userID)
	if err != nil {
		return nil, err
	}
	visible := make([]Execution, 0, len(executions))
	for _, execution := range executions {
		err := s.authz.RequireView(ctx, userID, execution.ManagedDatabaseID)
		if errors.Is(err, access.ErrInsufficientPermission) {
			continue
		}
		if err != nil {
			return nil, err
		}
		visible = append(visible, execution)
	}
	return visible, nil
}

func approvalSummary(ctx context.Context, repo approvalRepository, userID, executionID uint) (*ApprovalSummary, error) {
	execution, err := repo.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	approvals, err := repo.ListExecutionApprovals(ctx, executionID)
	if err != nil {
		return nil, err
	}
	summary := &ApprovalSummary{
		ExecutionID:     execution.ID,
		ExecutionStatus: execution.Status,
		Approvals:       approvals,
	}
	if execution.Status == ExecutionStatusPendingReview {
		for _, approval := range approvals {
			if approval.Decision == ApprovalDecisionPending && approval.LeaderUserID != nil && *approval.LeaderUserID == userID {
				summary.CanDecide = true
				break
			}
		}
	}
	return summary, nil
}

type approverGroup struct {
	ID           uint
	Name         string
	LeaderUserID *uint
}

func requiredApproverGroups(tx *gorm.DB, databaseID uint) ([]approverGroup, error) {
	var groups []approverGroup
	if err := tx.Table("group_database_permissions AS p").
		Select("g.id, g.name, g.leader_user_id").
		Joins("JOIN groups g ON g.id = p.group_id").
		Where("p.managed_database_id = ? AND p.permission >= ?", databaseID, access.PermissionViewOnly).
		Order("g.id").
		Scan(&groups).Error; err != nil {
		return nil, fmt.Errorf("finding groups that must approve: %w", err)
	}
	return groups, nil
}

func createApprovals(tx *gorm.DB, execution *Execution) error {
	groups, err := requiredApproverGroups(tx, execution.ManagedDatabaseID)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return ErrNoApprovers
	}
	var missing []string
	for _, group := range groups {
		if group.LeaderUserID == nil {
			missing = append(missing, group.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w (missing: %s)", ErrGroupLeaderMissing, strings.Join(missing, ", "))
	}
	return insertPendingApprovals(tx, execution.ID, groups)
}

func insertPendingApprovals(tx *gorm.DB, executionID uint, groups []approverGroup) error {
	approvals := make([]ExecutionApproval, 0, len(groups))
	for _, group := range groups {
		approvals = append(approvals, ExecutionApproval{
			ExecutionID: executionID,
			GroupID:     group.ID,
			GroupName:   group.Name,
			Decision:    ApprovalDecisionPending,
		})
	}
	if err := tx.Create(&approvals).Error; err != nil {
		return fmt.Errorf("creating migration approvals: %w", err)
	}
	return nil
}

func addMissingApprovals(tx *gorm.DB, execution *Execution) (bool, error) {
	groups, err := requiredApproverGroups(tx, execution.ManagedDatabaseID)
	if err != nil {
		return false, err
	}
	var existing []uint
	if err := tx.Model(&ExecutionApproval{}).
		Where("execution_id = ?", execution.ID).
		Pluck("group_id", &existing).Error; err != nil {
		return false, fmt.Errorf("listing migration approvals: %w", err)
	}
	known := make(map[uint]bool, len(existing))
	for _, id := range existing {
		known[id] = true
	}
	var missing []approverGroup
	for _, group := range groups {
		if !known[group.ID] {
			missing = append(missing, group)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	return true, insertPendingApprovals(tx, execution.ID, missing)
}

func reopenReview(tx *gorm.DB, execution *Execution, now time.Time) error {
	if err := tx.Model(execution).Updates(map[string]any{
		"status": ExecutionStatusPendingReview, "updated_at": now,
	}).Error; err != nil {
		return fmt.Errorf("reopening migration review: %w", err)
	}
	if execution.Type == ExecutionTypeUp {
		if err := tx.Model(&Draft{}).Where("id = ? AND status = ?", execution.DraftID, DraftStatusApproved).
			Updates(map[string]any{"status": DraftStatusPendingReview, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("reopening migration draft review: %w", err)
		}
	}
	return nil
}

func (r *GormRepository) DecideExecution(ctx context.Context, executionID, userID uint, decision ApprovalDecision, note string) error {
	if decision != ApprovalDecisionApproved && decision != ApprovalDecisionRejected {
		return fmt.Errorf("migration: invalid approval decision %q", decision)
	}
	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var execution Execution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&execution, executionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrExecutionNotFound
			}
			return fmt.Errorf("loading migration execution for review: %w", err)
		}
		if execution.Status != ExecutionStatusPendingReview {
			return ErrExecutionNotPendingReview
		}
		result := tx.Model(&ExecutionApproval{}).
			Where("execution_id = ? AND decision = ?", executionID, ApprovalDecisionPending).
			Where("group_id IN (SELECT id FROM groups WHERE leader_user_id = ?)", userID).
			Updates(map[string]any{
				"decision": decision, "decided_by": userID, "decided_at": now, "note": note, "updated_at": now,
			})
		if result.Error != nil {
			return fmt.Errorf("recording migration approval: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrNotApprover
		}

		status, draftStatus := ExecutionStatusRejected, DraftStatusDraft
		if decision == ApprovalDecisionApproved {
			var pending int64
			if err := tx.Model(&ExecutionApproval{}).
				Where("execution_id = ? AND decision = ?", executionID, ApprovalDecisionPending).
				Count(&pending).Error; err != nil {
				return fmt.Errorf("counting pending migration approvals: %w", err)
			}
			if pending > 0 {
				return nil
			}
			status, draftStatus = ExecutionStatusApproved, DraftStatusApproved
		}
		if err := tx.Model(&execution).Updates(map[string]any{"status": status, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("updating reviewed migration execution: %w", err)
		}
		if execution.Type == ExecutionTypeUp {
			if err := tx.Model(&Draft{}).Where("id = ? AND status = ?", execution.DraftID, DraftStatusPendingReview).
				Updates(map[string]any{"status": draftStatus, "updated_at": now}).Error; err != nil {
				return fmt.Errorf("updating reviewed migration draft: %w", err)
			}
		}
		if execution.Compensation && decision == ApprovalDecisionRejected {
			if execution.TargetExecutionID == nil {
				return ErrInvalidCompensation
			}
			result := tx.Model(&Execution{}).
				Where("id = ? AND status = ?", *execution.TargetExecutionID, ExecutionStatusCompensationPending).
				Updates(map[string]any{"status": ExecutionStatusPartial, "updated_at": now})
			if result.Error != nil {
				return fmt.Errorf("releasing partial migration after compensation rejection: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrInvalidCompensation
			}
		}
		return nil
	})
}

func (r *GormRepository) ListExecutionApprovals(ctx context.Context, executionID uint) ([]ApprovalView, error) {
	views := []ApprovalView{}
	if err := r.db.WithContext(ctx).
		Table("migration_execution_approvals AS a").
		Select(`a.group_id, a.group_name, g.leader_user_id, leader.username AS leader_username,
			a.decision, a.decided_by, decider.username AS decided_by_username, a.decided_at, a.note`).
		Joins("JOIN groups g ON g.id = a.group_id").
		Joins("LEFT JOIN users leader ON leader.id = g.leader_user_id").
		Joins("LEFT JOIN users decider ON decider.id = a.decided_by").
		Where("a.execution_id = ?", executionID).
		Order("a.group_id").
		Scan(&views).Error; err != nil {
		return nil, fmt.Errorf("listing migration approvals: %w", err)
	}
	return views, nil
}

func (r *GormRepository) ListPendingApprovals(ctx context.Context, userID uint) ([]Execution, error) {
	executions := []Execution{}
	if err := r.db.WithContext(ctx).
		Where("status = ?", ExecutionStatusPendingReview).
		Where(`EXISTS (
			SELECT 1 FROM migration_execution_approvals a
			JOIN groups g ON g.id = a.group_id
			WHERE a.execution_id = migration_executions.id
			AND a.decision = ? AND g.leader_user_id = ?
		)`, ApprovalDecisionPending, userID).
		Order("id ASC").
		Find(&executions).Error; err != nil {
		return nil, fmt.Errorf("listing pending migration approvals: %w", err)
	}
	return executions, nil
}
