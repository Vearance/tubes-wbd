package migration

import (
	"context"
	"sync"
	"time"
)

var _ Repository = (*fakeRepo)(nil)

type fakeRepo struct {
	mu         sync.Mutex
	drafts     map[uint]Draft
	runs       []Run
	executions map[uint]Execution
	nextID     uint
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{drafts: map[uint]Draft{}, executions: map[uint]Execution{}, nextID: 1}
}

func (r *fakeRepo) CreateDraft(_ context.Context, d *Draft) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d.ID = r.nextID
	r.nextID++
	r.drafts[d.ID] = *d
	return nil
}

func (r *fakeRepo) FindDraft(_ context.Context, id uint) (*Draft, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.drafts[id]
	if !ok {
		return nil, ErrDraftNotFound
	}
	return &d, nil
}

func (r *fakeRepo) ListDrafts(_ context.Context, managedDatabaseID uint) ([]Draft, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := []Draft{}
	for id := r.nextID - 1; id >= 1; id-- {
		if d, ok := r.drafts[id]; ok && d.ManagedDatabaseID == managedDatabaseID {
			list = append(list, d)
		}
	}
	return list, nil
}

func (r *fakeRepo) UpdateDraft(_ context.Context, d *Draft) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.drafts[d.ID]
	if !ok {
		return ErrDraftNotFound
	}
	if !stored.Status.open() {
		return ErrDraftNotEditable
	}
	d.Status = DraftStatusDraft
	stored.Name, stored.SQL, stored.DownSQL = d.Name, d.SQL, d.DownSQL
	stored.Irreversible, stored.IrreversibleReason = d.Irreversible, d.IrreversibleReason
	stored.UpdatedBy, stored.Status = d.UpdatedBy, d.Status
	r.drafts[d.ID] = stored
	return nil
}

func (r *fakeRepo) ClaimDraft(_ context.Context, id uint, from DraftStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.drafts[id]
	if !ok {
		return ErrDraftNotFound
	}
	if d.Status != from {
		return ErrDraftNotExecutable
	}
	d.Status = DraftStatusRunning
	r.drafts[id] = d
	return nil
}

func (r *fakeRepo) ReleaseDraft(_ context.Context, id uint, to DraftStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.drafts[id]; ok && d.Status == DraftStatusRunning {
		d.Status = to
		r.drafts[id] = d
	}
	return nil
}

func (r *fakeRepo) RecordRun(_ context.Context, run *Run, status DraftStatus, appliedVersion *int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.ID = uint(len(r.runs) + 1)
	r.runs = append(r.runs, *run)
	if d, ok := r.drafts[run.DraftID]; ok && d.Status == DraftStatusRunning {
		d.Status, d.AppliedVersion = status, appliedVersion
		r.drafts[run.DraftID] = d
	}
	return nil
}

func (r *fakeRepo) SubmitExecution(_ context.Context, e *Execution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.drafts[e.DraftID]
	if !ok {
		return ErrDraftNotFound
	}
	if !d.Status.open() {
		return ErrDraftNotExecutable
	}
	if e.Name != d.Name || e.SQL != d.SQL || e.DownSQL != d.DownSQL {
		return ErrDraftChangedDuringSubmission
	}
	e.ID = r.nextID
	r.nextID++
	r.executions[e.ID] = *e
	d.Status = DraftStatusPendingReview
	r.drafts[d.ID] = d
	return nil
}

func (r *fakeRepo) CreateExecution(_ context.Context, e *Execution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e.ID = r.nextID
	r.nextID++
	r.executions[e.ID] = *e
	return nil
}

func (r *fakeRepo) GetExecution(_ context.Context, id uint) (*Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok {
		return nil, ErrExecutionNotFound
	}
	return &e, nil
}

func (r *fakeRepo) ApproveExecution(_ context.Context, id uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok {
		return ErrExecutionNotFound
	}
	if e.Status != ExecutionStatusPendingReview {
		return ErrExecutionNotQueueable
	}
	e.Status = ExecutionStatusApproved
	r.executions[id] = e
	d := r.drafts[e.DraftID]
	d.Status = DraftStatusApproved
	r.drafts[d.ID] = d
	return nil
}

func (r *fakeRepo) EnqueueApprovedExecution(_ context.Context, id uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok {
		return ErrExecutionNotFound
	}
	if e.Status != ExecutionStatusApproved {
		return ErrExecutionNotApproved
	}
	e.Status = ExecutionStatusQueued
	r.executions[id] = e
	d := r.drafts[e.DraftID]
	d.Status = DraftStatusQueued
	r.drafts[d.ID] = d
	return nil
}

func (r *fakeRepo) ClaimExecution(_ context.Context, id uint) (*Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok || e.Status != ExecutionStatusQueued {
		return nil, nil
	}
	e.Status = ExecutionStatusRunning
	e.Attempt++
	r.executions[id] = e
	d := r.drafts[e.DraftID]
	d.Status = DraftStatusRunning
	r.drafts[d.ID] = d
	return &e, nil
}

func (r *fakeRepo) FinishExecution(_ context.Context, id uint, status ExecutionStatus, version *int64, code, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok {
		return ErrExecutionNotFound
	}
	if e.Status != ExecutionStatusRunning {
		return ErrExecutionNotQueueable
	}
	e.Status, e.Version, e.ErrorCode, e.ErrorMessage = status, version, code, message
	r.executions[id] = e
	d := r.drafts[e.DraftID]
	if status == ExecutionStatusSuccess {
		d.Status, d.AppliedVersion = DraftStatusApplied, version
	} else {
		d.Status = DraftStatusFailed
	}
	r.drafts[d.ID] = d
	return nil
}

func (r *fakeRepo) RetryExecution(_ context.Context, id uint, _ time.Duration, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok {
		return ErrExecutionNotFound
	}
	if e.Status != ExecutionStatusRunning {
		return ErrExecutionNotQueueable
	}
	e.Status, e.ErrorMessage = ExecutionStatusQueued, message
	r.executions[id] = e
	if e.Type == ExecutionTypeUp {
		d := r.drafts[e.DraftID]
		d.Status = DraftStatusQueued
		r.drafts[d.ID] = d
	}
	return nil
}

func (r *fakeRepo) ListStaleExecutions(_ context.Context, _ time.Time) ([]Execution, error) {
	return nil, nil
}

func (r *fakeRepo) ListExecutions(_ context.Context, dbID uint) ([]Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := []Execution{}
	for _, execution := range r.executions {
		if execution.ManagedDatabaseID == dbID {
			list = append(list, execution)
		}
	}
	return list, nil
}

func (r *fakeRepo) ListRuns(_ context.Context, draftID uint) ([]Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := []Run{}
	for _, run := range r.runs {
		if run.DraftID == draftID {
			list = append(list, run)
		}
	}
	return list, nil
}

func (r *fakeRepo) status(id uint) DraftStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.drafts[id].Status
}
