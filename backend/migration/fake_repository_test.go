package migration

import (
	"context"
	"sync"
)

var _ Repository = (*fakeRepo)(nil)

type fakeRepo struct {
	mu     sync.Mutex
	drafts map[uint]Draft
	runs   []Run
	nextID uint
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{drafts: map[uint]Draft{}, nextID: 1}
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
	stored.Name, stored.SQL, stored.UpdatedBy, stored.Status = d.Name, d.SQL, d.UpdatedBy, d.Status
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
