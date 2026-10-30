package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/workflow/workflowdb"
)

// RunSummary is a run in lists.
type RunSummary struct {
	ID                uuid.UUID       `json:"id"`
	Definition        string          `json:"definition"`
	DefinitionVersion int             `json:"definition_version"`
	ShopID            uuid.UUID       `json:"shop_id"`
	BatchID           *uuid.UUID      `json:"batch_id"`
	Status            string          `json:"status"`
	Input             json.RawMessage `json:"input"`
	Output            json.RawMessage `json:"output"`
	CreatedBy         *uuid.UUID      `json:"created_by"`
	CancelRequested   bool            `json:"cancel_requested"`
	CreatedAt         time.Time       `json:"created_at"`
	StartedAt         *time.Time      `json:"started_at"`
	FinishedAt        *time.Time      `json:"finished_at"`
}

// StepInfo is a step as shown to users.
type StepInfo struct {
	ID            uuid.UUID       `json:"id"`
	Key           string          `json:"key"`
	Position      int             `json:"position"`
	Status        string          `json:"status"`
	Blocking      bool            `json:"blocking"`
	Capability    *string         `json:"capability"`
	AttemptCount  int             `json:"attempt_count"`
	MaxAttempts   int             `json:"max_attempts"`
	NextAttemptAt *time.Time      `json:"next_attempt_at"`
	Output        json.RawMessage `json:"output"`
	Error         json.RawMessage `json:"error"`
	SkipReason    *string         `json:"skip_reason"`
	StartedAt     *time.Time      `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

// Progress counts steps by outcome.
type Progress struct {
	Total int `json:"total"`
	Done  int `json:"done"`
}

// RunView is a run with its steps and pending approval.
type RunView struct {
	Run             RunSummary    `json:"run"`
	Steps           []StepInfo    `json:"steps"`
	Progress        Progress      `json:"progress"`
	PendingApproval *ApprovalView `json:"pending_approval"`
}

// ApprovalView is an approval with the diff it was asked about.
type ApprovalView struct {
	ID              uuid.UUID       `json:"id"`
	RunID           uuid.UUID       `json:"run_id"`
	StepID          uuid.UUID       `json:"step_id"`
	ShopID          uuid.UUID       `json:"shop_id"`
	RunCreatedBy    *uuid.UUID      `json:"run_created_by"`
	Status          string          `json:"status"`
	Diff            json.RawMessage `json:"diff"`
	SelectedChanges []string        `json:"selected_changes"`
	DecidedBy       *uuid.UUID      `json:"decided_by"`
	Comment         *string         `json:"comment"`
	DecidedAt       *time.Time      `json:"decided_at"`
	CreatedAt       time.Time       `json:"created_at"`
}

// EventView is one run event.
type EventView struct {
	ID        uuid.UUID       `json:"id"`
	RunID     uuid.UUID       `json:"run_id"`
	StepID    *uuid.UUID      `json:"step_id"`
	Type      string          `json:"type"`
	Level     string          `json:"level"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func rawOrEmpty(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}

func toRun(r workflowdb.WorkflowRun) RunSummary {
	return RunSummary{ID: r.ID, Definition: r.DefinitionKey, DefinitionVersion: int(r.DefinitionVersion), ShopID: r.ShopConnectionID,
		BatchID: r.BatchID, Status: r.Status, Input: rawOrEmpty(r.Input), Output: rawOrEmpty(r.Output), CreatedBy: r.CreatedBy,
		CancelRequested: r.CancelRequestedAt != nil, CreatedAt: r.CreatedAt.UTC(), StartedAt: utc(r.StartedAt), FinishedAt: utc(r.FinishedAt)}
}

func toStep(s workflowdb.WorkflowStep) StepInfo {
	out := StepInfo{ID: s.ID, Key: s.StepKey, Position: int(s.Position), Status: s.Status, Blocking: s.Blocking, Capability: s.CapabilityKey,
		AttemptCount: int(s.AttemptCount), MaxAttempts: int(s.MaxAttempts), Output: rawOrEmpty(s.Output), Error: rawOrEmpty(s.Error),
		SkipReason: s.SkipReason, StartedAt: utc(s.StartedAt), FinishedAt: utc(s.FinishedAt)}
	if s.Status == StepPending && s.AttemptCount > 0 {
		out.NextAttemptAt = utc(&s.NextAttemptAt)
	}
	return out
}

func toApproval(a workflowdb.WorkflowApproval, r workflowdb.WorkflowRun) ApprovalView {
	v := ApprovalView{ID: a.ID, RunID: a.RunID, StepID: a.StepID, ShopID: r.ShopConnectionID, RunCreatedBy: r.CreatedBy, Status: a.Status,
		Diff: rawOrEmpty(a.DiffSnapshot), DecidedBy: a.DecidedBy, Comment: a.Comment, DecidedAt: utc(a.DecidedAt), CreatedAt: a.CreatedAt.UTC(),
		SelectedChanges: []string{}}
	if len(a.SelectedChanges) > 0 {
		_ = json.Unmarshal(a.SelectedChanges, &v.SelectedChanges)
	}
	return v
}

// GetRun returns a run with steps.
func (e *Engine) GetRun(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID uuid.UUID) (RunView, error) {
	var out RunView
	err := e.tx(ctx, t.OrgID, p.UserID, func(q *workflowdb.Queries, _ pgx.Tx) error {
		run, err := q.GetRun(ctx, workflowdb.GetRunParams{OrgID: t.OrgID, ID: runID})
		if err != nil {
			return wrap(err, errRunNotFound)
		}
		steps, err := q.ListSteps(ctx, workflowdb.ListStepsParams{OrgID: t.OrgID, RunID: runID})
		if err != nil {
			return err
		}
		out = RunView{Run: toRun(run), Steps: make([]StepInfo, 0, len(steps))}
		for _, s := range steps {
			out.Steps = append(out.Steps, toStep(s))
			out.Progress.Total++
			if IsTerminalStep(s.Status) {
				out.Progress.Done++
			}
			if s.Status == StepWaitingApproval {
				a, err := q.LatestApprovalForStep(ctx, workflowdb.LatestApprovalForStepParams{OrgID: t.OrgID, StepID: s.ID})
				if err == nil && a.Status == "PENDING" {
					v := toApproval(a, run)
					out.PendingApproval = &v
				}
			}
		}
		return nil
	})
	return out, wrap(err, nil)
}

// ListFilter narrows the run list.
type ListFilter struct {
	Status  *string
	ShopID  *uuid.UUID
	BatchID *uuid.UUID
	Limit   int
	Cursor  string
}

var runStatuses = []string{RunPending, RunRunning, RunWaitingApproval, RunWaitingUser, RunCompleted, RunPartiallyCompleted, RunFailed, RunCancelled}

// ListRuns lists runs, newest first, with a time cursor.
func (e *Engine) ListRuns(ctx context.Context, p authctx.Principal, t authctx.Tenant, f ListFilter) ([]RunSummary, *string, error) {
	if f.Status != nil && !contains(runStatuses, *f.Status) {
		return nil, nil, apperr.Invalid(apperr.FieldError{Field: "status", Code: "invalid", Message: "Unknown status."})
	}
	var before *time.Time
	if f.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		ts, perr := time.Parse(time.RFC3339Nano, string(b))
		if err != nil || perr != nil {
			return nil, nil, apperr.Invalid(apperr.FieldError{Field: "cursor", Code: "invalid", Message: "Invalid cursor."})
		}
		before = &ts
	}
	out := []RunSummary{}
	var next *string
	err := e.tx(ctx, t.OrgID, p.UserID, func(q *workflowdb.Queries, _ pgx.Tx) error {
		rows, err := q.ListRuns(ctx, workflowdb.ListRunsParams{OrgID: t.OrgID, Status: f.Status, ShopConnectionID: f.ShopID,
			BatchID: f.BatchID, Before: before, Lim: int32(f.Limit + 1)}) // #nosec G115 -- bounded by QueryInt
		for i, r := range rows {
			if i == f.Limit {
				c := base64.RawURLEncoding.EncodeToString([]byte(rows[i-1].CreatedAt.UTC().Format(time.RFC3339Nano)))
				next = &c
				break
			}
			out = append(out, toRun(r))
		}
		return err
	})
	return out, next, wrap(err, nil)
}

// Events lists a run's events after an opaque cursor (for polling).
func (e *Engine) Events(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID uuid.UUID, after string, limit int) ([]EventView, *string, error) {
	var afterAt *time.Time
	var afterID *uuid.UUID
	if after != "" {
		at, id, ok := decodeEventCursor(after)
		if !ok {
			return nil, nil, apperr.Invalid(apperr.FieldError{Field: "after", Code: "invalid", Message: "Invalid cursor."})
		}
		afterAt, afterID = &at, &id
	}
	out := []EventView{}
	err := e.tx(ctx, t.OrgID, p.UserID, func(q *workflowdb.Queries, _ pgx.Tx) error {
		if _, err := q.GetRun(ctx, workflowdb.GetRunParams{OrgID: t.OrgID, ID: runID}); err != nil {
			return wrap(err, errRunNotFound)
		}
		rows, err := q.ListRunEvents(ctx, workflowdb.ListRunEventsParams{OrgID: t.OrgID, RunID: runID, AfterAt: afterAt, AfterID: afterID,
			Lim: int32(limit)}) // #nosec G115 -- bounded by QueryInt
		for _, r := range rows {
			out = append(out, toEvent(r))
		}
		return err
	})
	var next *string
	if len(out) > 0 {
		c := encodeEventCursor(out[len(out)-1].CreatedAt, out[len(out)-1].ID)
		next = &c
	} else if after != "" {
		next = &after
	}
	return out, next, wrap(err, nil)
}

// OrgEvents lists recent events of all runs (execution log), newest first.
func (e *Engine) OrgEvents(ctx context.Context, p authctx.Principal, t authctx.Tenant, level *string, before string, limit int) ([]EventView, *string, error) {
	if level != nil && *level != "info" && *level != "warn" && *level != "error" {
		return nil, nil, apperr.Invalid(apperr.FieldError{Field: "level", Code: "invalid", Message: "Must be info, warn or error."})
	}
	var beforeAt *time.Time
	var beforeID *uuid.UUID
	if before != "" {
		at, id, ok := decodeEventCursor(before)
		if !ok {
			return nil, nil, apperr.Invalid(apperr.FieldError{Field: "cursor", Code: "invalid", Message: "Invalid cursor."})
		}
		beforeAt, beforeID = &at, &id
	}
	out := []EventView{}
	var next *string
	err := e.tx(ctx, t.OrgID, p.UserID, func(q *workflowdb.Queries, _ pgx.Tx) error {
		rows, err := q.ListOrgEvents(ctx, workflowdb.ListOrgEventsParams{OrgID: t.OrgID, Level: level, BeforeAt: beforeAt, BeforeID: beforeID,
			Lim: int32(limit + 1)}) // #nosec G115 -- bounded by QueryInt
		for i, r := range rows {
			if i == limit {
				c := encodeEventCursor(rows[i-1].CreatedAt, rows[i-1].ID)
				next = &c
				break
			}
			out = append(out, toEvent(r))
		}
		return err
	})
	return out, next, wrap(err, nil)
}

func toEvent(r workflowdb.WorkflowEvent) EventView {
	return EventView{ID: r.ID, RunID: r.RunID, StepID: r.StepID, Type: r.Type, Level: r.Level, Message: r.Message,
		Data: rawOrEmpty(r.Data), CreatedAt: r.CreatedAt.UTC()}
}

func encodeEventCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeEventCursor(s string) (time.Time, uuid.UUID, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	ts, idPart, ok := strings.Cut(string(b), "|")
	if !ok {
		return time.Time{}, uuid.Nil, false
	}
	t, err1 := time.Parse(time.RFC3339Nano, ts)
	id, err2 := uuid.Parse(idPart)
	return t, id, err1 == nil && err2 == nil
}

// ListApprovals lists approvals, newest first.
func (e *Engine) ListApprovals(ctx context.Context, p authctx.Principal, t authctx.Tenant, status *string) ([]ApprovalView, error) {
	if status != nil && !contains([]string{"PENDING", "APPROVED", "REJECTED", "EXPIRED"}, *status) {
		return nil, apperr.Invalid(apperr.FieldError{Field: "status", Code: "invalid", Message: "Unknown status."})
	}
	out := []ApprovalView{}
	err := e.tx(ctx, t.OrgID, p.UserID, func(q *workflowdb.Queries, _ pgx.Tx) error {
		rows, err := q.ListApprovals(ctx, workflowdb.ListApprovalsParams{OrgID: t.OrgID, Status: status})
		for _, r := range rows {
			a := workflowdb.WorkflowApproval{ID: r.ID, OrgID: r.OrgID, RunID: r.RunID, StepID: r.StepID, Status: r.Status,
				DiffSnapshot: r.DiffSnapshot, SelectedChanges: r.SelectedChanges, DecidedBy: r.DecidedBy, Comment: r.Comment,
				DecidedAt: r.DecidedAt, CreatedAt: r.CreatedAt}
			out = append(out, toApproval(a, workflowdb.WorkflowRun{ShopConnectionID: r.ShopConnectionID, CreatedBy: r.RunCreatedBy}))
		}
		return err
	})
	return out, wrap(err, nil)
}

// GetApproval returns one approval with its diff.
func (e *Engine) GetApproval(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) (ApprovalView, error) {
	var out ApprovalView
	err := e.tx(ctx, t.OrgID, p.UserID, func(q *workflowdb.Queries, _ pgx.Tx) error {
		a, err := q.GetApproval(ctx, workflowdb.GetApprovalParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errApprovalNotFound)
		}
		run, err := q.GetRun(ctx, workflowdb.GetRunParams{OrgID: t.OrgID, ID: a.RunID})
		out = toApproval(a, run)
		return err
	})
	return out, wrap(err, nil)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
