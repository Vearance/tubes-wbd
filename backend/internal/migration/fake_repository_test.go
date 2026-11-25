package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var _ Repository = (*fakeRepo)(nil)
var _ approvalRepository = (*fakeRepo)(nil)

type fakeGroup struct {
	id        uint
	name      string
	leader    *uint
	databases map[uint]bool
}

type fakeRepo struct {
	mu         sync.Mutex
	drafts     map[uint]Draft
	runs       []Run
	executions map[uint]Execution
	groups     map[uint]*fakeGroup
	approvals  map[uint][]ExecutionApproval
	nextID     uint
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		drafts:     map[uint]Draft{},
		executions: map[uint]Execution{},
		groups:     map[uint]*fakeGroup{},
		approvals:  map[uint][]ExecutionApproval{},
		nextID:     1,
	}
}

func (r *fakeRepo) addGroup(id uint, name string, leader *uint, databaseIDs ...uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	group := &fakeGroup{id: id, name: name, leader: leader, databases: map[uint]bool{}}
	for _, databaseID := range databaseIDs {
		group.databases[databaseID] = true
	}
	r.groups[id] = group
}

func (r *fakeRepo) setLeader(groupID uint, leader *uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groups[groupID].leader = leader
}

func (r *fakeRepo) requiredGroups(databaseID uint) []*fakeGroup {
	var groups []*fakeGroup
	for _, group := range r.groups {
		if group.databases[databaseID] {
			groups = append(groups, group)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].id < groups[j].id })
	return groups
}

func (r *fakeRepo) createApprovals(e *Execution) error {
	groups := r.requiredGroups(e.ManagedDatabaseID)
	if len(groups) == 0 {
		return ErrNoApprovers
	}
	var missing []string
	for _, group := range groups {
		if group.leader == nil {
			missing = append(missing, group.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w (missing: %s)", ErrGroupLeaderMissing, strings.Join(missing, ", "))
	}
	for _, group := range groups {
		r.approvals[e.ID] = append(r.approvals[e.ID], ExecutionApproval{
			ExecutionID: e.ID, GroupID: group.id, GroupName: group.name, Decision: ApprovalDecisionPending,
		})
	}
	return nil
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
	if err := r.createApprovals(e); err != nil {
		e.ID = 0
		return err
	}
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
	if err := r.createApprovals(e); err != nil {
		e.ID = 0
		return err
	}
	r.nextID++
	r.executions[e.ID] = *e
	return nil
}

func (r *fakeRepo) CreateCompensationExecution(_ context.Context, execution *Execution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if execution.TargetExecutionID == nil || !execution.Compensation || execution.Type != ExecutionTypeRollback {
		return ErrInvalidCompensation
	}
	target, ok := r.executions[*execution.TargetExecutionID]
	if !ok || target.ManagedDatabaseID != execution.ManagedDatabaseID || target.Type != ExecutionTypeUp ||
		target.Status != ExecutionStatusPartial || target.ActiveDatabaseID == nil ||
		*target.ActiveDatabaseID != target.ManagedDatabaseID || target.Batch == nil ||
		target.BatchCount == 0 || target.DownSQL == "" || target.Irreversible {
		return ErrInvalidCompensation
	}
	execution.ID = r.nextID
	if err := r.createApprovals(execution); err != nil {
		execution.ID = 0
		return err
	}
	r.nextID++
	r.executions[execution.ID] = *execution
	target.Status = ExecutionStatusCompensationPending
	r.executions[target.ID] = target
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

func (r *fakeRepo) DecideExecution(_ context.Context, executionID, userID uint, decision ApprovalDecision, note string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[executionID]
	if !ok {
		return ErrExecutionNotFound
	}
	if e.Status != ExecutionStatusPendingReview {
		return ErrExecutionNotPendingReview
	}
	now := time.Now()
	decided, pending := 0, 0
	approvals := r.approvals[executionID]
	for i := range approvals {
		if approvals[i].Decision != ApprovalDecisionPending {
			continue
		}
		group := r.groups[approvals[i].GroupID]
		if group != nil && group.leader != nil && *group.leader == userID {
			decider := userID
			approvals[i].Decision, approvals[i].DecidedBy, approvals[i].DecidedAt, approvals[i].Note = decision, &decider, &now, note
			decided++
			continue
		}
		pending++
	}
	if decided == 0 {
		return ErrNotApprover
	}
	status, draftStatus := ExecutionStatusRejected, DraftStatusDraft
	if decision == ApprovalDecisionApproved {
		if pending > 0 {
			return nil
		}
		status, draftStatus = ExecutionStatusApproved, DraftStatusApproved
	}
	e.Status = status
	r.executions[executionID] = e
	if e.Type == ExecutionTypeUp {
		d := r.drafts[e.DraftID]
		if d.Status == DraftStatusPendingReview {
			d.Status = draftStatus
			r.drafts[d.ID] = d
		}
	}
	if e.Compensation && decision == ApprovalDecisionRejected && e.TargetExecutionID != nil {
		target := r.executions[*e.TargetExecutionID]
		target.Status = ExecutionStatusPartial
		r.executions[target.ID] = target
	}
	return nil
}

func (r *fakeRepo) ListExecutionApprovals(_ context.Context, executionID uint) ([]ApprovalView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	views := []ApprovalView{}
	for _, approval := range r.approvals[executionID] {
		view := ApprovalView{
			GroupID: approval.GroupID, GroupName: approval.GroupName, Decision: approval.Decision,
			DecidedBy: approval.DecidedBy, DecidedAt: approval.DecidedAt, Note: approval.Note,
		}
		if group := r.groups[approval.GroupID]; group != nil {
			view.LeaderUserID = group.leader
		}
		views = append(views, view)
	}
	return views, nil
}

func (r *fakeRepo) ListPendingApprovals(_ context.Context, userID uint) ([]Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := []Execution{}
	for id := uint(1); id < r.nextID; id++ {
		e, ok := r.executions[id]
		if !ok || e.Status != ExecutionStatusPendingReview {
			continue
		}
		for _, approval := range r.approvals[id] {
			group := r.groups[approval.GroupID]
			if approval.Decision == ApprovalDecisionPending && group != nil && group.leader != nil && *group.leader == userID {
				list = append(list, e)
				break
			}
		}
	}
	return list, nil
}

func (r *fakeRepo) EnqueueApprovedExecution(_ context.Context, id uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok {
		return ErrExecutionNotFound
	}
	if e.Status != ExecutionStatusApproved && !(e.Status == ExecutionStatusPartial && e.Batch != nil) {
		return ErrExecutionNotApproved
	}
	known := map[uint]bool{}
	for _, approval := range r.approvals[id] {
		if approval.Decision != ApprovalDecisionApproved {
			return ErrExecutionNotApproved
		}
		known[approval.GroupID] = true
	}
	added := false
	for _, group := range r.requiredGroups(e.ManagedDatabaseID) {
		if !known[group.id] {
			r.approvals[id] = append(r.approvals[id], ExecutionApproval{
				ExecutionID: id, GroupID: group.id, GroupName: group.name, Decision: ApprovalDecisionPending,
			})
			added = true
		}
	}
	if added {
		e.Status = ExecutionStatusPendingReview
		r.executions[id] = e
		if d := r.drafts[e.DraftID]; e.Type == ExecutionTypeUp && d.Status == DraftStatusApproved {
			d.Status = DraftStatusPendingReview
			r.drafts[d.ID] = d
		}
		return ErrApprovalsOutdated
	}
	e.Status = ExecutionStatusQueued
	r.executions[id] = e
	if e.Type == ExecutionTypeUp {
		d := r.drafts[e.DraftID]
		d.Status = DraftStatusQueued
		r.drafts[d.ID] = d
	}
	return nil
}

func (r *fakeRepo) ClaimExecution(_ context.Context, id uint) (*Execution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok || e.Status != ExecutionStatusQueued {
		return nil, nil
	}
	if e.Compensation {
		if e.TargetExecutionID == nil {
			return nil, ErrInvalidCompensation
		}
		target := r.executions[*e.TargetExecutionID]
		switch target.Status {
		case ExecutionStatusCompensationPending:
			target.Status, target.ActiveDatabaseID = ExecutionStatusCompensating, nil
			r.executions[target.ID] = target
		case ExecutionStatusCompensating:
			if e.ActiveDatabaseID == nil || *e.ActiveDatabaseID != e.ManagedDatabaseID {
				return nil, ErrInvalidCompensation
			}
		default:
			return nil, ErrInvalidCompensation
		}
	}
	e.Status = ExecutionStatusRunning
	e.Attempt++
	databaseID := e.ManagedDatabaseID
	e.ActiveDatabaseID = &databaseID
	r.executions[id] = e
	if e.Type == ExecutionTypeUp {
		d := r.drafts[e.DraftID]
		d.Status = DraftStatusRunning
		r.drafts[d.ID] = d
	}
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
	if status == ExecutionStatusPartial {
		databaseID := e.ManagedDatabaseID
		e.ActiveDatabaseID = &databaseID
	} else {
		e.ActiveDatabaseID = nil
	}
	r.executions[id] = e
	if e.Type == ExecutionTypeUp {
		d := r.drafts[e.DraftID]
		if status == ExecutionStatusSuccess {
			d.Status, d.AppliedVersion = DraftStatusApplied, version
		} else {
			d.Status = DraftStatusFailed
		}
		r.drafts[d.ID] = d
	}
	if e.Compensation && status == ExecutionStatusSuccess && e.TargetExecutionID != nil {
		target := r.executions[*e.TargetExecutionID]
		target.Status, target.ActiveDatabaseID = ExecutionStatusCompensated, nil
		r.executions[target.ID] = target
	}
	return nil
}

func (r *fakeRepo) UpdateExecutionProgress(_ context.Context, id uint, batches int, rows int64, cursor *int64, done bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executions[id]
	if !ok {
		return ErrExecutionNotFound
	}
	e.BatchCount, e.RowsProcessed, e.LastCursor, e.BatchDone = batches, rows, cursor, done
	r.executions[id] = e
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
	if e.Batch != nil {
		databaseID := e.ManagedDatabaseID
		e.ActiveDatabaseID = &databaseID
	}
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
