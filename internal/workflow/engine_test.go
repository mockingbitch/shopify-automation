package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/platform/testdb"
	"shopee-automation/internal/rbac"
	"shopee-automation/internal/workflow/workflowdb"
)

type harness struct {
	t     *testing.T
	e     *Engine
	q     *queue.Inline
	clk   *clock.Fake
	org   uuid.UUID
	user  uuid.UUID
	shop  uuid.UUID
	shop2 uuid.UUID
}

// newHarness creates a database with one org, user and two shops.
func newHarness(t *testing.T) *harness {
	t.Helper()
	d := testdb.New(t)
	h := &harness{t: t, q: &queue.Inline{}, clk: clock.NewFake(time.Now()), org: db.NewID(), user: db.NewID(), shop: db.NewID(), shop2: db.NewID()}
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, testdb.OwnerURL(t, d))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Close(ctx) }()
	app := db.NewID()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, email, password_hash, display_name) VALUES ($1, 'u@example.com', 'x', 'U')`, []any{h.user}},
		{`INSERT INTO organizations (id, name, slug, created_by) VALUES ($1, 'Org', 'org-w', $2)`, []any{h.org, h.user}},
		{`INSERT INTO shopee_apps (id, name, environment, api_host, auth_host, partner_id, partner_key_enc, dek_wrapped, kek_id, app_type, redirect_uri)
		  VALUES ($1, 'A', 'sandbox', 'https://x', 'https://x', 1, '\x00', '\x00', 'k', 'ERP System', 'https://x')`, []any{app}},
		{`INSERT INTO shop_connections (id, org_id, shopee_app_id, shopee_shop_id, status) VALUES ($1, $2, $3, 1, 'ACTIVE')`, []any{h.shop, h.org, app}},
		{`INSERT INTO shop_connections (id, org_id, shopee_app_id, shopee_shop_id, status) VALUES ($1, $2, $3, 2, 'ACTIVE')`, []any{h.shop2, h.org, app}},
	} {
		if _, err := owner.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	h.e = &Engine{DB: d, Queue: h.q, Clock: h.clk, Audit: audit.Recorder{Clock: h.clk},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if os.Getenv("TEST_DATABASE_SYSTEM_URL") != "" {
		h.e.System = testdb.System(t, d)
	}
	h.q.Handlers = map[string]queue.Handler{queue.TypeStepExecute: h.e.HandleExecute}
	return h
}

func (h *harness) principal() (authctx.Principal, authctx.Tenant) {
	return authctx.Principal{UserID: h.user}, authctx.Tenant{OrgID: h.org, Role: rbac.Role("owner")}
}

func (h *harness) start(def string, shop uuid.UUID) uuid.UUID {
	h.t.Helper()
	var runID uuid.UUID
	err := h.e.DB.Tx(context.Background(), db.Scope{OrgID: h.org, UserID: h.user}, func(tx pgx.Tx) error {
		runs, err := h.e.CreateRuns(context.Background(), tx, StartOptions{OrgID: h.org, UserID: h.user},
			[]StartSpec{{Definition: def, ShopID: shop, Input: map[string]any{"n": 1}}})
		if err == nil {
			runID = runs[0].ID
		}
		return err
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.e.Kick(context.Background(), h.org, runID)
	return runID
}

func (h *harness) run(id uuid.UUID) RunView {
	h.t.Helper()
	p, t := h.principal()
	v, err := h.e.GetRun(context.Background(), p, t, id)
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

func (h *harness) step(id uuid.UUID, key string) StepInfo {
	h.t.Helper()
	for _, s := range h.run(id).Steps {
		if s.Key == key {
			return s
		}
	}
	h.t.Fatalf("no step %s", key)
	return StepInfo{}
}

func (h *harness) noQueueErrors() {
	h.t.Helper()
	if len(h.q.Errors) > 0 {
		h.t.Fatalf("queue errors: %v", h.q.Errors)
	}
}

func ok(out any) StepHandler {
	return func(context.Context, *StepContext) Result { return Succeeded(out) }
}

func TestRunToCompletion(t *testing.T) {
	h := newHarness(t)
	h.e.Register(Definition{Key: "T", Version: 1, Steps: []StepSpec{
		{Key: "a", Handler: ok(map[string]int{"value": 41})},
		{Key: "b", Handler: func(_ context.Context, sc *StepContext) Result {
			var a struct{ Value int }
			if err := sc.Output("a", &a); err != nil {
				return Permanent(err)
			}
			sc.Log("info", "adding one", nil)
			return Succeeded(map[string]int{"value": a.Value + 1})
		}},
	}})
	id := h.start("T", h.shop)
	h.noQueueErrors()
	v := h.run(id)
	if v.Run.Status != RunCompleted || v.Progress.Done != 2 || v.Run.FinishedAt == nil {
		t.Fatalf("run: %+v", v.Run)
	}
	if !jsonEq(h.step(id, "b").Output, `{"value":42}`) {
		t.Fatalf("output: %s", h.step(id, "b").Output)
	}
	p, tn := h.principal()
	evs, _, err := h.e.Events(context.Background(), p, tn, id, "", 100)
	if err != nil || len(evs) < 6 {
		t.Fatalf("events: %d %v", len(evs), err)
	}
	// Polling with the cursor returns nothing new.
	_, cur, _ := h.e.Events(context.Background(), p, tn, id, "", 100)
	more, _, _ := h.e.Events(context.Background(), p, tn, id, *cur, 100)
	if len(more) != 0 {
		t.Fatalf("events after cursor: %d", len(more))
	}
}

func TestDuplicateDeliveryRunsOnce(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	release := make(chan struct{})
	h.e.Register(Definition{Key: "T", Version: 1, Steps: []StepSpec{{Key: "a", Handler: func(context.Context, *StepContext) Result {
		calls.Add(1)
		<-release
		return Succeeded(nil)
	}}}})
	// Create without kicking, then deliver the same step five times at once.
	var runID uuid.UUID
	_ = h.e.DB.Tx(context.Background(), db.Scope{OrgID: h.org}, func(tx pgx.Tx) error {
		runs, err := h.e.CreateRuns(context.Background(), tx, StartOptions{OrgID: h.org}, []StartSpec{{Definition: "T", ShopID: h.shop}})
		runID = runs[0].ID
		return err
	})
	stepID := h.run(runID).Steps[0].ID
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.e.Execute(context.Background(), h.org, stepID); err != nil {
				t.Error(err)
			}
		}()
	}
	time.Sleep(200 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 || h.run(runID).Run.Status != RunCompleted {
		t.Fatalf("calls=%d status=%s", calls.Load(), h.run(runID).Run.Status)
	}
}

func TestRetryWithBackoff(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	h.e.Register(Definition{Key: "T", Version: 1, Steps: []StepSpec{{Key: "a", MaxAttempts: 3, BaseDelay: time.Second,
		Handler: func(context.Context, *StepContext) Result {
			if calls.Add(1) < 3 {
				return Retryable(errors.New("shopee busy"))
			}
			return Succeeded(nil)
		}}}})
	id := h.start("T", h.shop)
	s := h.step(id, "a")
	if s.Status != StepPending || s.NextAttemptAt == nil || h.run(id).Run.Status != RunRunning {
		t.Fatalf("after first failure: %+v", s)
	}
	// Not due yet: a delivery now does nothing.
	if h.q.RunDelayed(context.Background()) != 1 || calls.Load() != 1 {
		t.Fatalf("ran before backoff elapsed: %d", calls.Load())
	}
	for range 2 {
		h.clk.Advance(5 * time.Second)
		_ = h.e.Execute(context.Background(), h.org, s.ID)
	}
	h.noQueueErrors()
	if calls.Load() != 3 || h.run(id).Run.Status != RunCompleted {
		t.Fatalf("calls=%d status=%s", calls.Load(), h.run(id).Run.Status)
	}
}

func TestExhaustedAttemptsFailAndUserRetryOnlyRerunsFailed(t *testing.T) {
	h := newHarness(t)
	var aCalls, bCalls atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)
	h.e.Register(Definition{Key: "T", Version: 1, Steps: []StepSpec{
		{Key: "a", Handler: func(context.Context, *StepContext) Result { aCalls.Add(1); return Succeeded(nil) }},
		{Key: "b", Handler: func(context.Context, *StepContext) Result {
			bCalls.Add(1)
			if fail.Load() {
				return Permanent(errors.New("invalid shop name"))
			}
			return Succeeded(nil)
		}},
		{Key: "c", Handler: ok(nil)},
	}})
	id := h.start("T", h.shop)
	v := h.run(id)
	if v.Run.Status != RunFailed || h.step(id, "c").Status != StepPending {
		t.Fatalf("after failure: %s %s", v.Run.Status, h.step(id, "c").Status)
	}
	if !jsonEq(h.step(id, "b").Error, `{"message":"invalid shop name"}`) {
		t.Fatalf("error: %s", h.step(id, "b").Error)
	}
	p, tn := h.principal()
	if _, err := h.e.Resume(context.Background(), p, tn, id); err == nil {
		t.Fatal("resume on a failed run should conflict")
	}
	fail.Store(false)
	if _, err := h.e.Retry(context.Background(), p, tn, id); err != nil {
		t.Fatal(err)
	}
	h.noQueueErrors()
	if h.run(id).Run.Status != RunCompleted || aCalls.Load() != 1 || bCalls.Load() != 2 {
		t.Fatalf("after retry: %s a=%d b=%d", h.run(id).Run.Status, aCalls.Load(), bCalls.Load())
	}
	if _, err := h.e.Retry(context.Background(), p, tn, id); err == nil {
		t.Fatal("retry of a completed run should conflict")
	}
}

func TestContinueOnFailure(t *testing.T) {
	h := newHarness(t)
	h.e.Register(Definition{Key: "T", Version: 1, Steps: []StepSpec{
		{Key: "a", ContinueOnFailure: true, Handler: func(context.Context, *StepContext) Result { return Permanent(errors.New("one channel failed")) }},
		{Key: "b", Handler: ok(nil)},
	}})
	id := h.start("T", h.shop)
	if v := h.run(id); v.Run.Status != RunPartiallyCompleted || h.step(id, "b").Status != StepSucceeded {
		t.Fatalf("status: %s", v.Run.Status)
	}
}

func TestLeaseExpiryAndZombieFencing(t *testing.T) {
	h := newHarness(t)
	var calls atomic.Int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	h.e.Register(Definition{Key: "T", Version: 1, Steps: []StepSpec{{Key: "a", Timeout: time.Hour, Handler: func(context.Context, *StepContext) Result {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst // a hung worker
			return Succeeded(map[string]string{"by": "zombie"})
		}
		return Succeeded(map[string]string{"by": "second"})
	}}}})
	var runID uuid.UUID
	_ = h.e.DB.Tx(context.Background(), db.Scope{OrgID: h.org}, func(tx pgx.Tx) error {
		runs, err := h.e.CreateRuns(context.Background(), tx, StartOptions{OrgID: h.org}, []StartSpec{{Definition: "T", ShopID: h.shop}})
		runID = runs[0].ID
		return err
	})
	stepID := h.run(runID).Steps[0].ID
	done := make(chan error, 1)
	go func() { done <- h.e.Execute(context.Background(), h.org, stepID) }()
	<-firstStarted

	// While the lease is valid, another delivery does nothing.
	_ = h.e.Execute(context.Background(), h.org, stepID)
	if calls.Load() != 1 {
		t.Fatal("step ran twice while leased")
	}
	// The lease expires (worker presumed dead); the step is taken over.
	h.clk.Advance(2 * time.Hour)
	if err := h.e.Execute(context.Background(), h.org, stepID); err != nil {
		t.Fatal(err)
	}
	if s := h.step(runID, "a"); s.Status != StepSucceeded || !jsonEq(s.Output, `{"by":"second"}`) || s.AttemptCount != 2 {
		t.Fatalf("takeover: %+v %s", s, s.Output)
	}
	// The zombie finishes late: its result is dropped.
	close(releaseFirst)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s := h.step(runID, "a"); !jsonEq(s.Output, `{"by":"second"}`) {
		t.Fatalf("zombie overwrote result: %s", s.Output)
	}
	ctx := context.Background()
	var outcomes []string
	_ = h.e.DB.Tx(ctx, db.Scope{OrgID: h.org}, func(tx pgx.Tx) error {
		as, err := workflowdb.New(tx).ListAttempts(ctx, workflowdb.ListAttemptsParams{OrgID: h.org, StepID: stepID})
		for _, a := range as {
			outcomes = append(outcomes, a.Outcome)
		}
		return err
	})
	if len(outcomes) != 2 || outcomes[0] != "TIMED_OUT" || outcomes[1] != "SUCCEEDED" {
		t.Fatalf("attempts: %v", outcomes)
	}
}

func TestMutatingStepsSerializePerShop(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	started := make(chan struct{})
	var bCalls atomic.Int32
	h.e.Register(Definition{Key: "SLOW", Version: 1, Steps: []StepSpec{{Key: "w", Mutating: true, Handler: func(context.Context, *StepContext) Result {
		close(started)
		<-release
		return Succeeded(nil)
	}}}})
	h.e.Register(Definition{Key: "FAST", Version: 1, Steps: []StepSpec{{Key: "w", Mutating: true, Handler: func(context.Context, *StepContext) Result {
		bCalls.Add(1)
		return Succeeded(nil)
	}}}})
	// Create the slow run here (T.Fatal must stay on the test goroutine) and
	// execute it in the background.
	var slow uuid.UUID
	_ = h.e.DB.Tx(context.Background(), db.Scope{OrgID: h.org}, func(tx pgx.Tx) error {
		runs, err := h.e.CreateRuns(context.Background(), tx, StartOptions{OrgID: h.org}, []StartSpec{{Definition: "SLOW", ShopID: h.shop}})
		slow = runs[0].ID
		return err
	})
	go h.e.Kick(context.Background(), h.org, slow)
	<-started
	other := h.start("FAST", h.shop2) // another shop: not blocked
	same := h.start("FAST", h.shop)   // same shop: waits
	if h.run(other).Run.Status != RunCompleted || bCalls.Load() != 1 || h.run(same).Run.Status != RunPending {
		t.Fatalf("other=%s same=%s calls=%d", h.run(other).Run.Status, h.run(same).Run.Status, bCalls.Load())
	}
	close(release)
	time.Sleep(100 * time.Millisecond)
	h.clk.Advance(10 * time.Second)
	h.q.RunDelayed(context.Background())
	if h.run(same).Run.Status != RunCompleted || h.run(same).Steps[0].AttemptCount != 1 {
		t.Fatalf("same shop after release: %+v", h.run(same).Run)
	}
}

func approvalDef(selected *[]string) Definition {
	return Definition{Key: "T", Version: 1,
		ValidateSelection: func(diff json.RawMessage, sel []string) error {
			for _, s := range sel {
				if s != "x" && s != "y" {
					return errors.New("unknown change " + s)
				}
			}
			return nil
		},
		Steps: []StepSpec{
			{Key: "approve", Handler: func(_ context.Context, sc *StepContext) Result {
				if sc.Approval != nil && sc.Approval.Status == "APPROVED" {
					*selected = sc.Approval.Selected
					return Succeeded(map[string]any{"selected": sc.Approval.Selected})
				}
				return AwaitApproval([]string{"x", "y"})
			}},
			{Key: "apply", Handler: ok(nil)},
		}}
}

func TestApprovalApproveAndReject(t *testing.T) {
	h := newHarness(t)
	var selected []string
	h.e.Register(approvalDef(&selected))
	p, tn := h.principal()
	ctx := context.Background()

	id := h.start("T", h.shop)
	v := h.run(id)
	if v.Run.Status != RunWaitingApproval || v.PendingApproval == nil {
		t.Fatalf("waiting: %+v", v)
	}
	aid := v.PendingApproval.ID
	if _, err := h.e.Decide(ctx, p, tn, aid, DecisionInput{Decision: "APPROVE", SelectedChangePaths: []string{"z"}}, nil); err == nil {
		t.Fatal("unknown change accepted")
	}
	denied := func(context.Context, authctx.Principal, authctx.Tenant, workflowdb.WorkflowRun) error {
		return errors.New("no")
	}
	if _, err := h.e.Decide(ctx, p, tn, aid, DecisionInput{Decision: "APPROVE"}, denied); err == nil {
		t.Fatal("policy ignored")
	}
	if _, err := h.e.Decide(ctx, p, tn, aid, DecisionInput{Decision: "APPROVE", SelectedChangePaths: []string{"y"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.e.Decide(ctx, p, tn, aid, DecisionInput{Decision: "REJECT"}, nil); err == nil {
		t.Fatal("decided twice")
	}
	h.noQueueErrors()
	if h.run(id).Run.Status != RunCompleted || len(selected) != 1 || selected[0] != "y" {
		t.Fatalf("after approval: %s %v", h.run(id).Run.Status, selected)
	}

	id2 := h.start("T", h.shop)
	aid2 := h.run(id2).PendingApproval.ID
	if _, err := h.e.Decide(ctx, p, tn, aid2, DecisionInput{Decision: "REJECT"}, nil); err != nil {
		t.Fatal(err)
	}
	v = h.run(id2)
	if v.Run.Status != RunCancelled || h.step(id2, "apply").Status != StepCancelled || h.step(id2, "approve").Status != StepCancelled {
		t.Fatalf("after reject: %+v", v)
	}
}

func TestManualTasksResumeAndCancel(t *testing.T) {
	h := newHarness(t)
	var connected atomic.Bool
	h.e.Register(Definition{Key: "T", Version: 1, Steps: []StepSpec{
		{Key: "connect", Handler: func(context.Context, *StepContext) Result {
			if !connected.Load() {
				return NeedsUser("reconnect_required", "Reconnect the shop, then resume.")
			}
			return Succeeded(nil)
		}},
		{Key: "banner", Handler: func(context.Context, *StepContext) Result {
			return Manual([]ManualTask{{Key: "banner", Title: "Upload banner", Instructions: "Seller Centre > Shop Decoration"}}, map[string]int{"api_calls": 0})
		}},
		{Key: "after", Handler: ok(nil)},
	}})
	p, tn := h.principal()
	ctx := context.Background()
	id := h.start("T", h.shop)
	if h.run(id).Run.Status != RunWaitingUser {
		t.Fatalf("status: %s", h.run(id).Run.Status)
	}
	connected.Store(true)
	if _, err := h.e.Resume(ctx, p, tn, id); err != nil {
		t.Fatal(err)
	}
	banner := h.step(id, "banner")
	if banner.Status != StepWaitingUser || h.run(id).Run.Status != RunWaitingUser {
		t.Fatalf("banner: %+v", banner)
	}
	if _, err := h.e.Resume(ctx, p, tn, id); err == nil {
		t.Fatal("resume must not skip manual tasks")
	}
	if _, err := h.e.CompleteManual(ctx, p, tn, id, h.step(id, "after").ID, CompleteInput{}); err == nil {
		t.Fatal("completed a step without manual tasks")
	}
	note := "Uploaded in Seller Centre"
	if _, err := h.e.CompleteManual(ctx, p, tn, id, banner.ID, CompleteInput{Note: &note}); err != nil {
		t.Fatal(err)
	}
	h.noQueueErrors()
	if h.run(id).Run.Status != RunCompleted {
		t.Fatalf("after manual: %s", h.run(id).Run.Status)
	}

	// Cancel while waiting.
	connected.Store(false)
	id2 := h.start("T", h.shop)
	if _, err := h.e.Cancel(ctx, p, tn, id2, CancelInput{}); err != nil {
		t.Fatal(err)
	}
	if v := h.run(id2); v.Run.Status != RunCancelled || !v.Run.CancelRequested || h.step(id2, "after").Status != StepCancelled {
		t.Fatalf("cancelled: %+v", v.Run)
	}
	if _, err := h.e.Cancel(ctx, p, tn, id2, CancelInput{}); err == nil {
		t.Fatal("cancelled twice")
	}
}

func TestSweeperRecoversLostTasks(t *testing.T) {
	h := newHarness(t)
	if h.e.System == nil {
		h.e.System = testdb.System(t, h.e.DB)
	}
	h.e.Register(Definition{Key: "T", Version: 1, Steps: []StepSpec{{Key: "a", Handler: ok(nil)}, {Key: "b", Handler: ok(nil)}}})
	// Lose every task: the queue has no handler.
	h.q.Handlers = map[string]queue.Handler{}
	id := h.start("T", h.shop)
	ctx := context.Background()
	if n, err := h.e.Sweep(ctx); err != nil || n != 0 {
		t.Fatalf("fresh step swept too early: %d %v", n, err)
	}
	h.clk.Advance(time.Minute)
	n, err := h.e.Sweep(ctx)
	if err != nil || n != 1 { // only the first step is runnable
		t.Fatalf("sweep: %d %v", n, err)
	}
	h.q.Handlers = map[string]queue.Handler{queue.TypeStepExecute: h.e.HandleExecute}
	h.q.ResetIDs()
	for range 2 {
		h.clk.Advance(time.Minute)
		if _, err := h.e.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if h.run(id).Run.Status != RunCompleted {
		t.Fatalf("after sweeping: %s", h.run(id).Run.Status)
	}
}

func jsonEq(a json.RawMessage, b string) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal([]byte(b), &y) == nil && reflect.DeepEqual(x, y)
}
