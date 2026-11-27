package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"tubes-wbd/internal/database/targetdb"
	"tubes-wbd/internal/telemetry"
)

var ErrExecutionNotFound = errors.New("migration: execution not found")
var ErrExecutionNotApproved = errors.New("migration: execution has not been approved")
var ErrExecutionNotQueueable = errors.New("migration: execution is not awaiting approval")
var ErrExecutionBusy = errors.New("migration: another execution is active for the target database")
var ErrDraftChangedDuringSubmission = errors.New("migration: draft changed while submitting for review")

type executionRepository interface {
	SubmitExecution(context.Context, *Execution) error
	CreateExecution(context.Context, *Execution) error
	CreateCompensationExecution(context.Context, *Execution) error
	GetExecution(context.Context, uint) (*Execution, error)
	EnqueueApprovedExecution(context.Context, uint) error
	ClaimExecution(context.Context, uint) (*Execution, error)
	FinishExecution(context.Context, uint, ExecutionStatus, *int64, string, string) error
	UpdateExecutionProgress(context.Context, uint, int, int64, *int64, bool) error
	RetryExecution(context.Context, uint, time.Duration, string) error
	ListStaleExecutions(context.Context, time.Time) ([]Execution, error)
	ListExecutions(context.Context, uint) ([]Execution, error)
}

var _ executionRepository = (*GormRepository)(nil)

// SubmitDraftForReview snapshots a draft into an immutable execution. Approval
// policy is deliberately owned by the migration workflow component.
func (s *MigrationService) SubmitDraftForReview(ctx context.Context, userID, draftID uint) (*Execution, error) {
	d, err := s.repo.FindDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireEdit(ctx, userID, d.ManagedDatabaseID); err != nil {
		return nil, err
	}
	if !d.Status.open() {
		return nil, ErrDraftNotExecutable
	}
	m, err := validateDefinition(Migration{Name: d.Name, SQL: d.SQL, DownSQL: d.DownSQL, Batch: d.Batch, Irreversible: d.Irreversible, IrreversibleReason: d.IrreversibleReason})
	if err != nil {
		return nil, err
	}
	configSnapshot, err := s.targetConfigSnapshot(ctx, d.ManagedDatabaseID)
	if err != nil {
		return nil, fmt.Errorf("snapshotting target database config: %w", err)
	}
	executions, ok := s.repo.(executionRepository)
	if !ok {
		return nil, errors.New("migration: repository does not support asynchronous executions")
	}
	execution := &Execution{
		ManagedDatabaseID:     d.ManagedDatabaseID,
		DraftID:               d.ID,
		Type:                  ExecutionTypeUp,
		Status:                ExecutionStatusPendingReview,
		Name:                  m.Name,
		SQL:                   m.SQL,
		DownSQL:               m.DownSQL,
		Batch:                 m.Batch,
		Irreversible:          m.Irreversible,
		IrreversibleReason:    m.IrreversibleReason,
		EncryptedTargetConfig: configSnapshot,
		ExecutedBy:            userID,
	}
	actor := telemetry.ActorUser(userID)
	dbLabel := telemetry.TargetDatabaseLabel(d.ManagedDatabaseID)
	telemetry.SetRequestDatabase(ctx, dbLabel)
	submitCtx := telemetry.WithActor(telemetry.WithDatabase(ctx, dbLabel), actor)

	if err := executions.SubmitExecution(submitCtx, execution); err != nil {
		return nil, err
	}
	slog.InfoContext(submitCtx, "migration execution submitted for review",
		"event", "migration.execution.submitted",
		"database", dbLabel,
		"execution_id", execution.ID,
		"draft_id", execution.DraftID,
		"execution_type", string(execution.Type),
		"execution_status", string(execution.Status),
		"status", "ok",
	)
	return execution, nil
}

func (s *MigrationService) SubmitRollbackForReview(ctx context.Context, userID, dbID uint) (*Execution, error) {
	if err := s.authz.RequireEdit(ctx, userID, dbID); err != nil {
		return nil, err
	}
	var latest *HistoryEntry
	if err := s.readOnly(ctx, dbID, func(tx pgx.Tx) error {
		var err error
		latest, err = latestActiveExecutionHistory(ctx, tx)
		return err
	}); err != nil {
		return nil, err
	}
	if latest.DownSQL == "" || latest.ExecutionID == 0 {
		return nil, ErrInvalidRollback
	}
	return s.createActionExecution(ctx, userID, dbID, latest.ExecutionID, ExecutionTypeRollback, latest.DownSQL)
}

func (s *MigrationService) SubmitForwardForReview(ctx context.Context, userID, dbID uint) (*Execution, error) {
	if err := s.authz.RequireEdit(ctx, userID, dbID); err != nil {
		return nil, err
	}
	var rollback *HistoryEntry
	if err := s.readOnly(ctx, dbID, func(tx pgx.Tx) error {
		var err error
		rollback, err = forwardCandidate(ctx, tx)
		return err
	}); err != nil {
		return nil, err
	}
	if rollback.TargetExecutionID == nil {
		return nil, ErrInvalidForward
	}
	return s.createActionExecution(ctx, userID, dbID, *rollback.TargetExecutionID, ExecutionTypeForward, "")
}

func (s *MigrationService) createActionExecution(ctx context.Context, userID, dbID, targetID uint, action ExecutionType, sql string) (*Execution, error) {
	repo, ok := s.repo.(executionRepository)
	if !ok {
		return nil, errors.New("migration: repository does not support asynchronous executions")
	}
	target, err := repo.GetExecution(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if target.ManagedDatabaseID != dbID || target.Status != ExecutionStatusSuccess {
		return nil, ErrInvalidInput
	}
	name := target.Name
	if action == ExecutionTypeRollback {
		name = "Rollback: " + name
	} else {
		name = "Forward: " + name
		sql = target.SQL
	}
	m, err := validate(Migration{Name: name, SQL: sql})
	if err != nil {
		return nil, err
	}
	execution := &Execution{
		ManagedDatabaseID:     dbID,
		DraftID:               target.DraftID,
		Type:                  action,
		Status:                ExecutionStatusPendingReview,
		Name:                  m.Name,
		SQL:                   m.SQL,
		DownSQL:               target.SQL,
		Batch:                 target.Batch,
		Irreversible:          target.Irreversible,
		IrreversibleReason:    target.IrreversibleReason,
		ExecutedBy:            userID,
		TargetExecutionID:     &targetID,
		EncryptedTargetConfig: append([]byte(nil), target.EncryptedTargetConfig...),
	}
	if action == ExecutionTypeForward {
		execution.DownSQL = target.DownSQL
	}
	if target.Batch != nil {
		execution.Batch = target.Batch
	}
	actor := telemetry.ActorUser(userID)
	dbLabel := telemetry.TargetDatabaseLabel(dbID)
	telemetry.SetRequestDatabase(ctx, dbLabel)
	createCtx := telemetry.WithActor(telemetry.WithDatabase(ctx, dbLabel), actor)

	if err := repo.CreateExecution(createCtx, execution); err != nil {
		return nil, err
	}
	slog.InfoContext(createCtx, "migration execution submitted for review",
		"event", "migration.execution.submitted",
		"database", dbLabel,
		"execution_id", execution.ID,
		"draft_id", execution.DraftID,
		"execution_type", string(execution.Type),
		"execution_status", string(execution.Status),
		"status", "ok",
	)
	return execution, nil
}

func (s *MigrationService) SubmitCompensationForReview(ctx context.Context, userID, executionID uint) (*Execution, error) {
	repo, ok := s.repo.(executionRepository)
	if !ok {
		return nil, errors.New("migration: repository does not support asynchronous executions")
	}
	target, err := repo.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireEdit(ctx, userID, target.ManagedDatabaseID); err != nil {
		return nil, err
	}
	if target.Type != ExecutionTypeUp || target.Status != ExecutionStatusPartial ||
		target.Batch == nil || target.BatchCount == 0 || target.DownSQL == "" || target.Irreversible {
		return nil, ErrInvalidCompensation
	}

	name := "Compensate: " + target.Name
	definition, err := validateDefinition(Migration{
		Name:    name,
		SQL:     target.DownSQL,
		DownSQL: target.SQL,
		Batch:   target.Batch,
	})
	if err != nil {
		return nil, fmt.Errorf("validating batch compensation: %w", err)
	}
	targetID := target.ID
	compensation := &Execution{
		ManagedDatabaseID:     target.ManagedDatabaseID,
		DraftID:               target.DraftID,
		Type:                  ExecutionTypeRollback,
		Status:                ExecutionStatusPendingReview,
		Name:                  definition.Name,
		SQL:                   definition.SQL,
		DownSQL:               definition.DownSQL,
		Batch:                 definition.Batch,
		ExecutedBy:            userID,
		TargetExecutionID:     &targetID,
		Compensation:          true,
		EncryptedTargetConfig: append([]byte(nil), target.EncryptedTargetConfig...),
	}
	actor := telemetry.ActorUser(userID)
	dbLabel := telemetry.TargetDatabaseLabel(target.ManagedDatabaseID)
	telemetry.SetRequestDatabase(ctx, dbLabel)
	compCtx := telemetry.WithActor(telemetry.WithDatabase(ctx, dbLabel), actor)

	if err := repo.CreateCompensationExecution(compCtx, compensation); err != nil {
		return nil, err
	}
	slog.InfoContext(compCtx, "migration compensation submitted for review",
		"event", "migration.execution.submitted",
		"database", dbLabel,
		"execution_id", compensation.ID,
		"draft_id", compensation.DraftID,
		"execution_type", string(compensation.Type),
		"execution_status", string(compensation.Status),
		"compensation", true,
		"status", "ok",
	)
	return compensation, nil
}

func (s *MigrationService) Execution(ctx context.Context, userID, executionID uint) (*Execution, error) {
	repo, ok := s.repo.(executionRepository)
	if !ok {
		return nil, errors.New("migration: repository does not support asynchronous executions")
	}
	execution, err := repo.GetExecution(ctx, executionID)
	if err != nil {
		return nil, err
	}
	if err := s.authz.RequireView(ctx, userID, execution.ManagedDatabaseID); err != nil {
		return nil, err
	}
	return execution, nil
}

func (s *MigrationService) Executions(ctx context.Context, userID, dbID uint) ([]Execution, error) {
	repo, ok := s.repo.(executionRepository)
	if !ok {
		return nil, errors.New("migration: repository does not support asynchronous executions")
	}
	if err := s.authz.RequireView(ctx, userID, dbID); err != nil {
		return nil, err
	}
	return repo.ListExecutions(ctx, dbID)
}

func (r *GormRepository) SubmitExecution(ctx context.Context, e *Execution) error {
	return telemetry.RunGORMTransaction(ctx, r.db, "migration.submit_execution", func(tx *gorm.DB) error {
		var draft Draft
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&draft, e.DraftID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDraftNotFound
			}
			return fmt.Errorf("locking migration draft for submission: %w", err)
		}
		if draft.Status != DraftStatusDraft && draft.Status != DraftStatusFailed {
			return ErrDraftNotExecutable
		}
		if e.Name != draft.Name || e.SQL != draft.SQL || e.DownSQL != draft.DownSQL || !sameBatch(e.Batch, draft.Batch) {
			return ErrDraftChangedDuringSubmission
		}
		draft.Status = DraftStatusPendingReview
		if err := tx.Model(&draft).Update("status", draft.Status).Error; err != nil {
			return fmt.Errorf("marking migration draft pending review: %w", err)
		}
		if err := tx.Create(e).Error; err != nil {
			return fmt.Errorf("creating migration execution: %w", err)
		}
		return createApprovals(tx, e)
	})
}

func (r *GormRepository) CreateExecution(ctx context.Context, e *Execution) error {
	return telemetry.RunGORMTransaction(ctx, r.db, "migration.create_execution", func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return fmt.Errorf("creating migration execution: %w", err)
		}
		return createApprovals(tx, e)
	})
}

func (r *GormRepository) CreateCompensationExecution(ctx context.Context, execution *Execution) error {
	if execution.TargetExecutionID == nil || !execution.Compensation || execution.Type != ExecutionTypeRollback {
		return ErrInvalidCompensation
	}
	now := time.Now()
	return telemetry.RunGORMTransaction(ctx, r.db, "migration.create_compensation_execution", func(tx *gorm.DB) error {
		var target Execution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, *execution.TargetExecutionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidCompensation
			}
			return fmt.Errorf("locking partial migration for compensation: %w", err)
		}
		if target.ManagedDatabaseID != execution.ManagedDatabaseID ||
			target.Type != ExecutionTypeUp || target.Status != ExecutionStatusPartial ||
			target.ActiveDatabaseID == nil || *target.ActiveDatabaseID != target.ManagedDatabaseID ||
			target.Batch == nil || target.BatchCount == 0 || target.DownSQL == "" || target.Irreversible {
			return ErrInvalidCompensation
		}
		if err := tx.Create(execution).Error; err != nil {
			return fmt.Errorf("creating migration compensation execution: %w", err)
		}
		if err := createApprovals(tx, execution); err != nil {
			return err
		}
		result := tx.Model(&Execution{}).
			Where("id = ? AND status = ? AND active_database_id = ?", target.ID, ExecutionStatusPartial, target.ManagedDatabaseID).
			Updates(map[string]any{"status": ExecutionStatusCompensationPending, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("reserving partial migration for compensation: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrInvalidCompensation
		}
		return nil
	})
}

func (r *GormRepository) GetExecution(ctx context.Context, id uint) (*Execution, error) {
	var execution Execution
	result := r.db.WithContext(ctx).First(&execution, id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrExecutionNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("querying migration execution: %w", result.Error)
	}
	return &execution, nil
}

// EnqueueApprovedExecution returns ErrApprovalsOutdated after committing new
// pending approvals when a group gained access since submission.
func (r *GormRepository) EnqueueApprovedExecution(ctx context.Context, id uint) error {
	now := time.Now()
	outdated := false
	err := telemetry.RunGORMTransaction(ctx, r.db, "migration.enqueue_execution", func(tx *gorm.DB) error {
		var execution Execution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&execution, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrExecutionNotFound
			}
			return fmt.Errorf("loading approved migration execution: %w", err)
		}
		resumingPartialBatch := execution.Batch != nil && execution.ActiveDatabaseID != nil
		if execution.Status != ExecutionStatusApproved && !(execution.Status == ExecutionStatusPartial && execution.Batch != nil) {
			return ErrExecutionNotApproved
		}
		var undecided int64
		if err := tx.Model(&ExecutionApproval{}).
			Where("execution_id = ? AND decision <> ?", id, ApprovalDecisionApproved).
			Count(&undecided).Error; err != nil {
			return fmt.Errorf("checking migration approvals: %w", err)
		}
		if undecided > 0 {
			return ErrExecutionNotApproved
		}
		added, err := addMissingApprovals(tx, &execution)
		if err != nil {
			return err
		}
		if added {
			outdated = true
			return reopenReview(tx, &execution, now)
		}
		if err := tx.Model(&execution).Updates(map[string]any{
			"status": ExecutionStatusQueued, "queued_at": now, "available_at": now, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("queueing approved migration execution: %w", err)
		}
		if execution.Type == ExecutionTypeUp {
			allowedDraftStates := []DraftStatus{DraftStatusApproved}
			if resumingPartialBatch {
				allowedDraftStates = append(allowedDraftStates, DraftStatusFailed)
			}
			result := tx.Model(&Draft{}).Where("id = ? AND status IN ?", execution.DraftID, allowedDraftStates).
				Update("status", DraftStatusQueued)
			if result.Error != nil {
				return fmt.Errorf("queueing approved migration draft: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrDraftNotExecutable
			}
		}
		if err := tx.Create(&ExecutionOutbox{
			ExecutionID:  id,
			TraceContext: telemetry.InjectTraceContext(ctx),
			AvailableAt:  now,
		}).Error; err != nil {
			return fmt.Errorf("creating migration execution outbox record: %w", err)
		}
		slog.InfoContext(ctx, "migration execution queued",
			"event", "migration.execution.queued",
			"database", telemetry.TargetDatabaseLabel(execution.ManagedDatabaseID),
			"execution_id", id,
			"draft_id", execution.DraftID,
			"execution_type", string(execution.Type),
			"status", string(ExecutionStatusQueued),
		)
		return nil
	})
	if err != nil {
		return err
	}
	if outdated {
		return ErrApprovalsOutdated
	}
	return nil
}

func (r *GormRepository) ClaimExecution(ctx context.Context, id uint) (*Execution, error) {
	var execution Execution
	err := telemetry.RunGORMTransaction(ctx, r.db, "migration.claim_execution", func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ?", id, ExecutionStatusQueued).
			Where("available_at IS NULL OR available_at <= ?", time.Now()).
			First(&execution)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return result.Error
		}

		busyStatuses := []ExecutionStatus{
			ExecutionStatusRunning,
			ExecutionStatusPartial,
			ExecutionStatusCompensationPending,
			ExecutionStatusCompensating,
		}
		busyQuery := tx.Model(&Execution{}).
			Where("managed_database_id = ? AND status IN ?", execution.ManagedDatabaseID, busyStatuses)
		if execution.Compensation && execution.TargetExecutionID != nil {
			busyQuery = busyQuery.Where("id <> ?", *execution.TargetExecutionID)
		}
		var busy int64
		if err := busyQuery.Count(&busy).Error; err != nil {
			return fmt.Errorf("checking for active target database execution: %w", err)
		}
		if busy > 0 {
			return ErrExecutionBusy
		}

		if execution.Compensation {
			if execution.TargetExecutionID == nil {
				return ErrInvalidCompensation
			}
			var target Execution
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, *execution.TargetExecutionID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrInvalidCompensation
				}
				return fmt.Errorf("locking partial migration during compensation claim: %w", err)
			}
			if target.ManagedDatabaseID != execution.ManagedDatabaseID || target.Type != ExecutionTypeUp {
				return ErrInvalidCompensation
			}
			switch target.Status {
			case ExecutionStatusCompensationPending:
				result = tx.Model(&Execution{}).
					Where("id = ? AND status = ? AND active_database_id = ?", target.ID, ExecutionStatusCompensationPending, target.ManagedDatabaseID).
					Updates(map[string]any{"status": ExecutionStatusCompensating, "active_database_id": nil, "updated_at": time.Now()})
				if result.Error != nil {
					return fmt.Errorf("transferring active migration lock to compensation: %w", result.Error)
				}
				if result.RowsAffected != 1 {
					return ErrInvalidCompensation
				}
			case ExecutionStatusCompensating:
				if execution.ActiveDatabaseID == nil || *execution.ActiveDatabaseID != execution.ManagedDatabaseID {
					return ErrInvalidCompensation
				}
			default:
				return ErrInvalidCompensation
			}
		}

		now := time.Now()
		result = tx.Model(&Execution{}).
			Where("id = ? AND status = ?", execution.ID, ExecutionStatusQueued).
			Updates(map[string]any{
				"status":             ExecutionStatusRunning,
				"active_database_id": execution.ManagedDatabaseID,
				"attempt":            gorm.Expr("attempt + 1"),
				"started_at":         now,
				"updated_at":         now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrExecutionNotQueueable
		}
		if execution.Type == ExecutionTypeUp {
			result = tx.Model(&Draft{}).Where("id = ? AND status = ?", execution.DraftID, DraftStatusQueued).
				Update("status", DraftStatusRunning)
			if result.Error != nil {
				return fmt.Errorf("claiming migration draft: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrDraftNotExecutable
			}
		}
		execution.Status = ExecutionStatusRunning
		execution.Attempt++
		execution.StartedAt = &now
		attempt := ExecutionAttempt{
			ExecutionID: execution.ID,
			Number:      execution.Attempt,
			Status:      ExecutionStatusRunning,
			StartedAt:   now,
		}
		if err := tx.Create(&attempt).Error; err != nil {
			return fmt.Errorf("recording migration execution attempt: %w", err)
		}
		return nil
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "ux_migration_executions_active_database_id" {
			return nil, ErrExecutionBusy
		}
		return nil, fmt.Errorf("claiming migration execution: %w", err)
	}
	if execution.ID == 0 {
		return nil, nil
	}
	return &execution, nil
}

func (r *GormRepository) UpdateExecutionProgress(ctx context.Context, id uint, batches int, rows int64, cursor *int64, done bool) error {
	result := r.db.WithContext(ctx).Model(&Execution{}).
		Where("id = ? AND status = ?", id, ExecutionStatusRunning).
		Updates(map[string]any{
			"batch_count": batches, "rows_processed": rows, "last_cursor": cursor, "batch_done": done,
		})
	if result.Error != nil {
		return fmt.Errorf("updating migration batch progress: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrExecutionNotQueueable
	}
	return nil
}

func (r *GormRepository) FinishExecution(ctx context.Context, id uint, status ExecutionStatus, version *int64, code, message string) error {
	if status != ExecutionStatusSuccess && status != ExecutionStatusFailed && status != ExecutionStatusPartial {
		return fmt.Errorf("migration: invalid terminal execution status %q", status)
	}
	now := time.Now()
	return telemetry.RunGORMTransaction(ctx, r.db, "migration.finish_execution", func(tx *gorm.DB) error {
		var execution Execution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&execution, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrExecutionNotFound
			}
			return fmt.Errorf("loading running migration execution: %w", err)
		}
		if execution.Status != ExecutionStatusRunning {
			return ErrExecutionNotQueueable
		}
		activeDatabaseID := any(nil)
		if status == ExecutionStatusPartial {
			activeDatabaseID = execution.ManagedDatabaseID
		}
		if err := tx.Model(&execution).Updates(map[string]any{
			"status": status, "active_database_id": activeDatabaseID, "version": version, "error_code": code,
			"error_message": message, "finished_at": now, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("finishing migration execution: %w", err)
		}
		attemptResult := tx.Model(&ExecutionAttempt{}).
			Where("execution_id = ? AND number = ? AND status = ?", execution.ID, execution.Attempt, ExecutionStatusRunning).
			Updates(map[string]any{
				"status": status, "version": version, "error_code": code,
				"error_message": message, "finished_at": now,
			})
		if attemptResult.Error != nil {
			return fmt.Errorf("finishing migration execution attempt: %w", attemptResult.Error)
		}
		if attemptResult.RowsAffected != 1 {
			return ErrExecutionNotQueueable
		}
		if execution.Type == ExecutionTypeUp {
			draftStatus := DraftStatusFailed
			if status == ExecutionStatusSuccess {
				draftStatus = DraftStatusApplied
			}
			result := tx.Model(&Draft{}).Where("id = ? AND status = ?", execution.DraftID, DraftStatusRunning).
				Updates(map[string]any{"status": draftStatus, "applied_version": version, "updated_at": now})
			if result.Error != nil {
				return fmt.Errorf("finishing migration draft: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrDraftNotExecutable
			}
		}
		if execution.Compensation && status == ExecutionStatusSuccess {
			if execution.TargetExecutionID == nil {
				return ErrInvalidCompensation
			}
			result := tx.Model(&Execution{}).
				Where("id = ? AND status = ?", *execution.TargetExecutionID, ExecutionStatusCompensating).
				Updates(map[string]any{"status": ExecutionStatusCompensated, "active_database_id": nil, "updated_at": now})
			if result.Error != nil {
				return fmt.Errorf("marking compensated migration complete: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrInvalidCompensation
			}
		}
		return nil
	})
}

func (r *GormRepository) RetryExecution(ctx context.Context, id uint, delay time.Duration, message string) error {
	availableAt := time.Now().Add(delay)
	return telemetry.RunGORMTransaction(ctx, r.db, "migration.retry_execution", func(tx *gorm.DB) error {
		var execution Execution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&execution, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrExecutionNotFound
			}
			return fmt.Errorf("loading execution for retry: %w", err)
		}
		if execution.Status != ExecutionStatusRunning {
			return ErrExecutionNotQueueable
		}
		now := time.Now()
		attemptResult := tx.Model(&ExecutionAttempt{}).
			Where("execution_id = ? AND number = ? AND status = ?", execution.ID, execution.Attempt, ExecutionStatusRunning).
			Updates(map[string]any{"status": ExecutionStatusFailed, "error_message": message, "finished_at": now})
		if attemptResult.Error != nil {
			return fmt.Errorf("recording failed migration attempt: %w", attemptResult.Error)
		}
		if attemptResult.RowsAffected != 1 {
			return ErrExecutionNotQueueable
		}
		activeDatabaseID := any(nil)
		if execution.Batch != nil {
			activeDatabaseID = execution.ManagedDatabaseID
		}
		if err := tx.Model(&execution).Updates(map[string]any{
			"status": ExecutionStatusQueued, "active_database_id": activeDatabaseID,
			"available_at": availableAt, "started_at": nil, "error_message": message,
			"updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("requeueing migration execution: %w", err)
		}
		if execution.Type == ExecutionTypeUp {
			result := tx.Model(&Draft{}).Where("id = ? AND status = ?", execution.DraftID, DraftStatusRunning).
				Update("status", DraftStatusQueued)
			if result.Error != nil {
				return fmt.Errorf("requeueing migration draft: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				return ErrDraftNotExecutable
			}
		}
		if err := tx.Create(&ExecutionOutbox{
			ExecutionID:  id,
			TraceContext: telemetry.InjectTraceContext(ctx),
			AvailableAt:  availableAt,
		}).Error; err != nil {
			return fmt.Errorf("creating retry outbox record: %w", err)
		}
		return nil
	})
}

func (r *GormRepository) ListStaleExecutions(ctx context.Context, before time.Time) ([]Execution, error) {
	var executions []Execution
	if err := r.db.WithContext(ctx).
		Where("status = ? AND started_at < ?", ExecutionStatusRunning, before).
		Order("started_at ASC").Find(&executions).Error; err != nil {
		return nil, fmt.Errorf("listing stale migration executions: %w", err)
	}
	return executions, nil
}

func (r *GormRepository) ListExecutions(ctx context.Context, dbID uint) ([]Execution, error) {
	var executions []Execution
	if err := r.db.WithContext(ctx).
		Where("managed_database_id = ?", dbID).
		Order("id DESC").Find(&executions).Error; err != nil {
		return nil, fmt.Errorf("listing migration executions: %w", err)
	}
	return executions, nil
}

// ExecutionWorker processes queued executions until cancellation. A worker
// only claims approved executions after the workflow has explicitly queued them.
type ExecutionWorker struct {
	service *MigrationService
	repo    executionRepository
}

func NewExecutionWorker(service *MigrationService, repository Repository) (*ExecutionWorker, error) {
	if service == nil {
		return nil, errors.New("migration worker: service is nil")
	}
	repo, ok := repository.(executionRepository)
	if !ok {
		return nil, errors.New("migration worker: repository does not support executions")
	}
	return &ExecutionWorker{service: service, repo: repo}, nil
}

func (w *ExecutionWorker) RunExecution(ctx context.Context, executionID uint) error {
	if err := w.RecoverStale(ctx); err != nil {
		return err
	}
	execution, err := w.repo.ClaimExecution(ctx, executionID)
	if err != nil || execution == nil {
		return err
	}

	var queueWait time.Duration
	if execution.AvailableAt != nil {
		queueWait = time.Since(*execution.AvailableAt)
	} else if execution.QueuedAt != nil {
		queueWait = time.Since(*execution.QueuedAt)
	}
	if queueWait < 0 {
		queueWait = 0
	}
	metrics := telemetry.Metrics()
	metrics.RecordMigrationQueueWait(ctx, string(execution.Type), queueWait)

	actor := telemetry.ActorUser(execution.ExecutedBy)
	dbLabel := telemetry.TargetDatabaseLabel(execution.ManagedDatabaseID)
	meta := telemetry.MigrationLogMeta{
		ExecutionID:   execution.ID,
		DraftID:       execution.DraftID,
		ExecutionType: string(execution.Type),
		Attempt:       execution.Attempt,
	}

	spanCtx, span := telemetry.Tracer("tubes-wbd/migration").Start(ctx, "migration.execution.run",
		trace.WithAttributes(
			attribute.Int64("migration.execution_id", int64(execution.ID)),
			attribute.Int64("migration.draft_id", int64(execution.DraftID)),
			attribute.Int64("migration.database_id", int64(execution.ManagedDatabaseID)),
			attribute.String("migration.type", string(execution.Type)),
			attribute.Int("migration.attempt", execution.Attempt),
			attribute.Bool("migration.batched", execution.Batch != nil),
			attribute.String("actor", actor),
			attribute.Int64("migration.queue_wait_ms", queueWait.Milliseconds()),
		),
	)
	execStart := time.Now()
	defer span.End()

	spanCtx = telemetry.WithActor(telemetry.WithDatabase(telemetry.WithMigrationMeta(spanCtx, meta), dbLabel), actor)

	workCtx, cancel := context.WithTimeout(context.WithoutCancel(spanCtx), w.service.opts.StatementTimeout+10*time.Second)
	defer cancel()

	slog.InfoContext(workCtx, "migration execution started",
		"event", "migration.execution.started",
		"execution_type", string(execution.Type),
		"status", "started",
		"queue_wait_ms", queueWait.Milliseconds(),
	)
	if err := w.service.authz.RequireEdit(workCtx, execution.ExecutedBy, execution.ManagedDatabaseID); err != nil {
		if execution.Batch != nil {
			completed, reconcileErr := w.reconcileBatchExecution(workCtx, execution)
			if reconcileErr != nil {
				return errors.Join(err, reconcileErr)
			}
			if completed {
				return nil
			}
		}
		status := executionFailureStatus(execution)
		if finishErr := w.repo.FinishExecution(workCtx, execution.ID, status, nil, "permission_revoked", err.Error()); finishErr != nil {
			span.RecordError(finishErr)
			span.SetStatus(codes.Error, telemetry.SanitizeError(finishErr))
			return errors.Join(err, finishErr)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "permission_revoked")
		duration := time.Since(execStart)
		metrics.RecordMigrationExecution(workCtx, string(execution.Type), string(status), duration)
		slog.WarnContext(workCtx, "migration execution rejected by authorization",
			"event", "migration.execution.failed",
			"execution_type", string(execution.Type),
			"execution_status", string(status),
			"status", "error",
			"error_code", "permission_revoked",
			"duration_ms", telemetry.RoundMillis(duration),
			"error", err,
		)
		return nil
	}
	entry, runErr := w.service.runExecution(workCtx, execution)
	if runErr != nil {
		if execution.Batch != nil {
			completed, reconcileErr := w.reconcileBatchExecution(workCtx, execution)
			if reconcileErr != nil {
				if !errors.Is(runErr, errBatchReconciliationRequired) &&
					retryableExecutionError(runErr) && execution.Attempt < maxExecutionAttempts {
					delay := time.Second << (execution.Attempt - 1)
					if err := w.repo.RetryExecution(workCtx, execution.ID, delay, runErr.Error()); err != nil {
						return errors.Join(runErr, reconcileErr, err)
					}
					return nil
				}
				return errors.Join(runErr, reconcileErr)
			}
			if completed {
				return nil
			}
		}
		if retryableExecutionError(runErr) && execution.Attempt < maxExecutionAttempts {
			delay := time.Second << (execution.Attempt - 1)
			retryAttrs := []any{
				"event", "migration.execution.retry_scheduled",
				"execution_type", string(execution.Type),
				"status", "retrying",
				"retry_in_ms", delay.Milliseconds(),
				"duration_ms", telemetry.RoundMillis(time.Since(execStart)),
				"error", runErr,
			}
			if sqlstate := telemetry.SQLState(runErr); sqlstate != "" {
				retryAttrs = append(retryAttrs, "sqlstate", sqlstate)
			}
			slog.WarnContext(workCtx, "migration execution attempt failed; retry scheduled", retryAttrs...)
			metrics.RecordMigrationRetry(workCtx, string(execution.Type), "retryable_error")
			span.RecordError(runErr)
			if err := w.repo.RetryExecution(workCtx, execution.ID, delay, runErr.Error()); err != nil {
				span.SetStatus(codes.Error, telemetry.SanitizeError(err))
				return errors.Join(runErr, err)
			}
			return nil
		}
		var executionErr *ExecutionError
		code, message := "execution_error", runErr.Error()
		if errors.As(runErr, &executionErr) {
			code, message = executionErr.Code, executionErr.Message
		}
		terminalStatus := executionFailureStatus(execution)
		if finishErr := w.repo.FinishExecution(workCtx, execution.ID, terminalStatus, nil, code, message); finishErr != nil {
			span.RecordError(finishErr)
			span.SetStatus(codes.Error, telemetry.SanitizeError(finishErr))
			return errors.Join(runErr, finishErr)
		}
		span.RecordError(runErr)
		span.SetStatus(codes.Error, code)
		duration := time.Since(execStart)
		metrics.RecordMigrationExecution(workCtx, string(execution.Type), string(terminalStatus), duration)
		errAttrs := []any{
			"event", "migration.execution.failed",
			"execution_type", string(execution.Type),
			"execution_status", string(terminalStatus),
			"status", "error",
			"error_code", code,
			"duration_ms", telemetry.RoundMillis(duration),
			"error", runErr,
		}
		if sqlstate := telemetry.SQLState(runErr); sqlstate != "" {
			errAttrs = append(errAttrs, "sqlstate", sqlstate)
		}
		slog.ErrorContext(workCtx, "migration execution failed", errAttrs...)
		return nil
	}
	if err := w.repo.FinishExecution(workCtx, execution.ID, ExecutionStatusSuccess, &entry.Version, "", ""); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, telemetry.SanitizeError(err))
		return err
	}
	metrics.RecordMigrationExecution(workCtx, string(execution.Type), string(ExecutionStatusSuccess), entry.Duration)
	slog.InfoContext(workCtx, "migration execution succeeded",
		"event", "migration.execution.completed",
		"execution_type", string(execution.Type),
		"execution_status", string(ExecutionStatusSuccess),
		"status", "ok",
		"version", entry.Version,
		"duration_ms", telemetry.RoundMillis(entry.Duration),
	)
	return nil
}

const maxExecutionAttempts = 3

func (w *ExecutionWorker) RecoverStale(ctx context.Context) error {
	cutoff := time.Now().Add(-w.service.opts.StatementTimeout - 10*time.Second)
	executions, err := w.repo.ListStaleExecutions(telemetry.WithSkipDBTelemetry(ctx), cutoff)
	if err != nil {
		return err
	}
	if len(executions) == 0 {
		return nil
	}

	recCtx, span := telemetry.Tracer("tubes-wbd/migration").Start(ctx, "migration.worker.recover_stale",
		trace.WithAttributes(attribute.Int("migration.stale_count", len(executions))),
	)
	recCtx = telemetry.WithActor(recCtx, "system:worker")
	defer span.End()

	for i := range executions {
		execution := &executions[i]
		dbLabel := telemetry.TargetDatabaseLabel(execution.ManagedDatabaseID)
		execCtx := telemetry.WithDatabase(telemetry.WithMigrationMeta(recCtx, telemetry.MigrationLogMeta{
			ExecutionID:   execution.ID,
			DraftID:       execution.DraftID,
			ExecutionType: string(execution.Type),
			Attempt:       execution.Attempt,
		}), dbLabel)
		if err := w.recoverStaleExecution(execCtx, execution); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, telemetry.SanitizeError(err))
			return err
		}
	}
	return nil
}

func (w *ExecutionWorker) recoverStaleExecution(ctx context.Context, execution *Execution) error {
	pool, closePool, err := w.service.executionPool(ctx, execution)
	if err != nil {
		return err
	}
	if closePool {
		defer pool.Close()
	}
	if err := ensureHistorySchema(ctx, pool, false); err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin stale execution recovery: %w", err)
	}
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", lockName).Scan(&locked); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("checking target lock during execution recovery: %w", err)
	}
	if !locked {
		_ = tx.Rollback(ctx)
		return nil
	}
	entry, err := w.reconcileTargetExecutionState(ctx, tx, execution)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if entry != nil {
		if err := w.repo.FinishExecution(ctx, execution.ID, ExecutionStatusSuccess, &entry.Version, "", ""); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		_ = tx.Rollback(ctx)
		return nil
	}
	_ = tx.Rollback(ctx)
	if execution.Attempt >= maxExecutionAttempts {
		terminalStatus := executionFailureStatus(execution)
		var elapsed time.Duration
		if execution.StartedAt != nil {
			elapsed = time.Since(*execution.StartedAt)
		}
		if err := w.repo.FinishExecution(ctx, execution.ID, terminalStatus, nil, "worker_interrupted", "worker stopped before completing the execution"); err != nil {
			return err
		}
		telemetry.Metrics().RecordMigrationExecution(ctx, string(execution.Type), string(terminalStatus), elapsed)
		slog.ErrorContext(ctx, "stale migration execution failed after max attempts",
			"event", "migration.stale.failed",
			"execution_type", string(execution.Type),
			"execution_status", string(terminalStatus),
			"status", "error",
			"duration_ms", telemetry.RoundMillis(elapsed),
		)
		return nil
	}
	if err := w.repo.RetryExecution(ctx, execution.ID, 0, "worker restarted before execution completed"); err != nil {
		return err
	}
	telemetry.Metrics().RecordMigrationRetry(ctx, string(execution.Type), "stale_recovery")
	slog.WarnContext(ctx, "recovering stale migration execution",
		"event", "migration.stale.recovered",
		"status", "retrying",
	)
	return nil
}

func (w *ExecutionWorker) reconcileBatchExecution(ctx context.Context, execution *Execution) (bool, error) {
	pool, closePool, err := w.service.executionPool(ctx, execution)
	if err != nil {
		return false, batchReconciliationError("connecting to target database", err)
	}
	if closePool {
		defer pool.Close()
	}
	if err := ensureHistorySchema(ctx, pool, false); err != nil {
		return false, batchReconciliationError("checking target migration history", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, batchReconciliationError("starting target checkpoint reconciliation", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtext($1))", lockName).Scan(&locked); err != nil {
		return false, batchReconciliationError("checking target migration lock", err)
	}
	if !locked {
		return false, batchReconciliationError("target migration lock is still held", ErrMigrationInProgress)
	}

	entry, err := w.reconcileTargetExecutionState(ctx, tx, execution)
	if err != nil {
		return false, err
	}
	if entry == nil {
		return false, nil
	}
	if err := w.repo.FinishExecution(ctx, execution.ID, ExecutionStatusSuccess, &entry.Version, "", ""); err != nil {
		return false, batchReconciliationError("finishing execution found in target history", err)
	}
	return true, nil
}

func (w *ExecutionWorker) reconcileTargetExecutionState(
	ctx context.Context,
	tx pgx.Tx,
	execution *Execution,
) (*HistoryEntry, error) {
	var completed *HistoryEntry
	exists, err := historyExists(ctx, tx)
	if err != nil {
		return nil, err
	}
	if exists {
		entry, lookupErr := getExecutionHistoryByID(ctx, tx, execution.ID)
		if lookupErr == nil {
			completed = entry
		} else if !errors.Is(lookupErr, ErrVersionNotFound) {
			return nil, lookupErr
		}
	}

	if execution.Batch == nil {
		return completed, nil
	}
	progress, exists, err := readBatchProgressForReconciliation(ctx, tx, execution)
	if err != nil {
		return nil, err
	}
	if !exists {
		progress = batchProgress{}
	}
	if batchProgressRegressed(execution, progress) {
		return nil, fmt.Errorf("target batch checkpoint is behind the Platform DB checkpoint")
	}
	applyBatchProgress(execution, progress)
	if err := w.service.updateExecutionProgress(ctx, execution); err != nil {
		return nil, batchReconciliationError("syncing target checkpoint to Platform DB", err)
	}
	return completed, nil
}

func readBatchProgressForReconciliation(
	ctx context.Context,
	tx pgx.Tx,
	execution *Execution,
) (batchProgress, bool, error) {
	var tableExists bool
	if err := tx.QueryRow(ctx, "SELECT to_regclass('_dbm.migration_batch_progress') IS NOT NULL").Scan(&tableExists); err != nil {
		return batchProgress{}, false, batchReconciliationError("checking target batch checkpoint table", err)
	}
	if !tableExists {
		if execution.BatchCount > 0 || execution.RowsProcessed > 0 || execution.BatchDone {
			return batchProgress{}, false, batchReconciliationError(
				"target batch checkpoint table is missing despite Platform DB progress",
				errTargetCommitOutcomeUnknown,
			)
		}
		return batchProgress{}, false, nil
	}

	progress, err := readBatchProgressTx(ctx, tx, execution.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		if execution.BatchCount > 0 || execution.RowsProcessed > 0 || execution.BatchDone {
			return batchProgress{}, false, batchReconciliationError(
				"target batch checkpoint row is missing despite Platform DB progress",
				errTargetCommitOutcomeUnknown,
			)
		}
		return batchProgress{}, false, nil
	}
	if err != nil {
		return batchProgress{}, false, batchReconciliationError("reading target batch checkpoint", err)
	}
	return progress, true, nil
}

func batchProgressRegressed(execution *Execution, progress batchProgress) bool {
	if progress.BatchCount < execution.BatchCount || progress.RowsProcessed < execution.RowsProcessed ||
		execution.BatchDone && !progress.Completed {
		return true
	}
	if execution.LastCursor != nil && progress.BatchCount == execution.BatchCount && progress.LastCursor != *execution.LastCursor {
		return true
	}
	return false
}

func executionFailureStatus(execution *Execution) ExecutionStatus {
	if execution.Compensation || execution.Batch != nil && (execution.BatchCount > 0 || execution.RowsProcessed > 0) {
		return ExecutionStatusPartial
	}
	return ExecutionStatusFailed
}

func retryableExecutionError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && transientSQLState(pgErr.Code) {
		return true
	}
	return pgconn.SafeToRetry(err) ||
		errors.Is(err, errBatchReconciliationRequired) ||
		errors.Is(err, targetdb.ErrUnreachable) ||
		errors.Is(err, targetdb.ErrTimeout) ||
		errors.Is(err, targetdb.ErrConnectionFailed) ||
		errors.Is(err, ErrMigrationInProgress)
}

func transientSQLState(code string) bool {
	return code == "40001" || code == "40P01"
}
