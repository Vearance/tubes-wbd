package migration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"tubes-wbd/internal/database/targetdb"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrExecutionNotFound = errors.New("migration: execution not found")
var ErrExecutionNotApproved = errors.New("migration: execution has not been approved")
var ErrExecutionNotQueueable = errors.New("migration: execution is not awaiting approval")
var ErrExecutionBusy = errors.New("migration: another execution is active for the target database")
var ErrDraftChangedDuringSubmission = errors.New("migration: draft changed while submitting for review")

type executionRepository interface {
	SubmitExecution(context.Context, *Execution) error
	CreateExecution(context.Context, *Execution) error
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
	executions, ok := s.repo.(executionRepository)
	if !ok {
		return nil, errors.New("migration: repository does not support asynchronous executions")
	}
	execution := &Execution{
		ManagedDatabaseID:  d.ManagedDatabaseID,
		DraftID:            d.ID,
		Type:               ExecutionTypeUp,
		Status:             ExecutionStatusPendingReview,
		Name:               m.Name,
		SQL:                m.SQL,
		DownSQL:            m.DownSQL,
		Batch:              m.Batch,
		Irreversible:       m.Irreversible,
		IrreversibleReason: m.IrreversibleReason,
		ExecutedBy:         userID,
	}
	if err := executions.SubmitExecution(ctx, execution); err != nil {
		return nil, err
	}
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
	var latest *HistoryEntry
	if err := s.readOnly(ctx, dbID, func(tx pgx.Tx) error {
		var err error
		latest, err = latestExecutionHistory(ctx, tx)
		return err
	}); err != nil {
		return nil, err
	}
	if latest.Type != ExecutionTypeRollback || latest.TargetExecutionID == nil {
		return nil, ErrInvalidForward
	}
	return s.createActionExecution(ctx, userID, dbID, *latest.TargetExecutionID, ExecutionTypeForward, "")
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
		ManagedDatabaseID:  dbID,
		DraftID:            target.DraftID,
		Type:               action,
		Status:             ExecutionStatusPendingReview,
		Name:               m.Name,
		SQL:                m.SQL,
		DownSQL:            target.SQL,
		Batch:              target.Batch,
		Irreversible:       target.Irreversible,
		IrreversibleReason: target.IrreversibleReason,
		ExecutedBy:         userID,
		TargetExecutionID:  &targetID,
	}
	if action == ExecutionTypeForward {
		execution.DownSQL = target.DownSQL
	}
	if target.Batch != nil {
		execution.Batch = target.Batch
	}
	if err := repo.CreateExecution(ctx, execution); err != nil {
		return nil, err
	}
	return execution, nil
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
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return fmt.Errorf("creating migration execution: %w", err)
		}
		return createApprovals(tx, e)
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
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if err := tx.Create(&ExecutionOutbox{ExecutionID: id, AvailableAt: now}).Error; err != nil {
			return fmt.Errorf("creating migration execution outbox record: %w", err)
		}
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
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND status = ?", id, ExecutionStatusQueued).
			Where("available_at IS NULL OR available_at <= ?", time.Now()).
			Where(`NOT EXISTS (
				SELECT 1 FROM migration_executions running
				WHERE running.managed_database_id = migration_executions.managed_database_id
				AND running.status IN ?
			)`, []ExecutionStatus{ExecutionStatusRunning, ExecutionStatusPartial}).
			First(&execution)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			var queued Execution
			queuedResult := tx.Where("id = ? AND status = ?", id, ExecutionStatusQueued).First(&queued)
			if errors.Is(queuedResult.Error, gorm.ErrRecordNotFound) {
				return nil
			}
			if queuedResult.Error != nil {
				return queuedResult.Error
			}
			if queued.AvailableAt != nil && queued.AvailableAt.After(time.Now()) {
				return nil
			}
			var busy bool
			if err := tx.Raw(`SELECT EXISTS (
				SELECT 1 FROM migration_executions
				WHERE managed_database_id = ? AND status IN ?
			)`, queued.ManagedDatabaseID, []ExecutionStatus{ExecutionStatusRunning, ExecutionStatusPartial}).Scan(&busy).Error; err != nil {
				return err
			}
			if busy {
				return ErrExecutionBusy
			}
			return nil
		}
		if result.Error != nil {
			return result.Error
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
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		return nil
	})
}

func (r *GormRepository) RetryExecution(ctx context.Context, id uint, delay time.Duration, message string) error {
	availableAt := time.Now().Add(delay)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if err := tx.Create(&ExecutionOutbox{ExecutionID: id, AvailableAt: availableAt}).Error; err != nil {
			return fmt.Errorf("creating retry outbox record: %w", err)
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
		if err := tx.Create(&ExecutionOutbox{ExecutionID: id, AvailableAt: availableAt}).Error; err != nil {
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
	slog.Info("migration execution started",
		"execution_id", execution.ID,
		"database_id", execution.ManagedDatabaseID,
		"execution_type", execution.Type,
		"attempt", execution.Attempt,
	)
	workCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.service.opts.StatementTimeout+10*time.Second)
	defer cancel()
	if err := w.service.authz.RequireEdit(workCtx, execution.ExecutedBy, execution.ManagedDatabaseID); err != nil {
		status := ExecutionStatusFailed
		if execution.Batch != nil {
			status = ExecutionStatusPartial
		}
		if finishErr := w.repo.FinishExecution(workCtx, execution.ID, status, nil, "permission_revoked", err.Error()); finishErr != nil {
			return errors.Join(err, finishErr)
		}
		return nil
	}
	entry, runErr := w.service.runExecution(workCtx, execution)
	if runErr != nil {
		if retryableExecutionError(runErr) && execution.Attempt < maxExecutionAttempts {
			delay := time.Second << (execution.Attempt - 1)
			slog.Warn("migration execution attempt failed; retry scheduled",
				"execution_id", execution.ID,
				"database_id", execution.ManagedDatabaseID,
				"execution_type", execution.Type,
				"attempt", execution.Attempt,
				"retry_in", delay,
				"error", runErr,
			)
			if err := w.repo.RetryExecution(workCtx, execution.ID, delay, runErr.Error()); err != nil {
				return errors.Join(runErr, err)
			}
			return nil
		}
		var executionErr *ExecutionError
		code, message := "execution_error", runErr.Error()
		if errors.As(runErr, &executionErr) {
			code, message = executionErr.Code, executionErr.Message
		}
		terminalStatus := ExecutionStatusFailed
		if execution.Batch != nil {
			terminalStatus = ExecutionStatusPartial
		}
		if finishErr := w.repo.FinishExecution(workCtx, execution.ID, terminalStatus, nil, code, message); finishErr != nil {
			return errors.Join(runErr, finishErr)
		}
		slog.Error("migration execution failed",
			"execution_id", execution.ID,
			"database_id", execution.ManagedDatabaseID,
			"execution_type", execution.Type,
			"attempt", execution.Attempt,
			"error_code", code,
			"error", runErr,
		)
		return nil
	}
	if err := w.repo.FinishExecution(workCtx, execution.ID, ExecutionStatusSuccess, &entry.Version, "", ""); err != nil {
		return err
	}
	slog.Info("migration execution succeeded",
		"execution_id", execution.ID,
		"database_id", execution.ManagedDatabaseID,
		"execution_type", execution.Type,
		"attempt", execution.Attempt,
		"version", entry.Version,
		"duration", entry.Duration,
	)
	return nil
}

const maxExecutionAttempts = 3

func (w *ExecutionWorker) RecoverStale(ctx context.Context) error {
	cutoff := time.Now().Add(-w.service.opts.StatementTimeout - 10*time.Second)
	executions, err := w.repo.ListStaleExecutions(ctx, cutoff)
	if err != nil {
		return err
	}
	for i := range executions {
		execution := &executions[i]
		pool, err := w.service.pools.Pool(ctx, execution.ManagedDatabaseID)
		if err != nil {
			return err
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
			continue
		}
		exists, err := historyExists(ctx, tx)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if exists {
			entry, lookupErr := getExecutionHistoryByID(ctx, tx, execution.ID)
			if lookupErr == nil {
				if err := w.repo.FinishExecution(ctx, execution.ID, ExecutionStatusSuccess, &entry.Version, "", ""); err != nil {
					_ = tx.Rollback(ctx)
					return err
				}
				_ = tx.Rollback(ctx)
				continue
			}
			if !errors.Is(lookupErr, ErrVersionNotFound) {
				_ = tx.Rollback(ctx)
				return lookupErr
			}
		}
		_ = tx.Rollback(ctx)
		if execution.Attempt >= maxExecutionAttempts {
			terminalStatus := ExecutionStatusFailed
			if execution.Batch != nil {
				terminalStatus = ExecutionStatusPartial
			}
			if err := w.repo.FinishExecution(ctx, execution.ID, terminalStatus, nil, "worker_interrupted", "worker stopped before completing the execution"); err != nil {
				return err
			}
			continue
		}
		if err := w.repo.RetryExecution(ctx, execution.ID, 0, "worker restarted before execution completed"); err != nil {
			return err
		}
	}
	return nil
}

func retryableExecutionError(err error) bool {
	return errors.Is(err, targetdb.ErrUnreachable) ||
		errors.Is(err, targetdb.ErrTimeout) ||
		errors.Is(err, targetdb.ErrConnectionFailed) ||
		errors.Is(err, ErrMigrationInProgress)
}
