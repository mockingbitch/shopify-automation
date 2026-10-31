package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/browser/browserdb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/media"
)

// Task statuses.
const (
	TaskQueued      = "QUEUED"
	TaskRunning     = "RUNNING"
	TaskWaitingUser = "WAITING_USER"
	TaskSucceeded   = "SUCCEEDED"
	TaskFailed      = "FAILED"
	TaskCancelled   = "CANCELLED"
)

const (
	leaseTTL          = 10 * time.Minute
	inputURLTTL       = 10 * time.Minute
	maxScreenshots    = 10
	maxScreenshotSize = 5 << 20
	maxLogEntries     = 200
)

// Ready implements shops.ExtensionReadiness: the org has a paired device,
// and these capabilities have an enabled recipe for the market.
func (s *Service) Ready(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, market string) (bool, map[string]bool, error) {
	q := browserdb.New(tx)
	n, err := q.CountActiveDevices(ctx, browserdb.CountActiveDevicesParams{OrgID: orgID, Now: s.now()})
	if err != nil {
		return false, nil, err
	}
	keys, err := q.EnabledRecipeKeys(ctx, market)
	if err != nil {
		return false, nil, err
	}
	ready := map[string]bool{}
	for _, k := range keys {
		ready[k] = true
	}
	return n > 0, ready, nil
}

// TaskSpec is what a workflow step asks the extension to do.
type TaskSpec struct {
	OrgID      uuid.UUID
	ShopID     uuid.UUID
	RunID      uuid.UUID
	StepID     uuid.UUID
	Key        string
	Capability string
	Market     string
	Title      string
	// Target is the approved value (a string, a list of URLs, or an object).
	Target any
}

// storedFile is a task input file kept in object storage.
type storedFile struct {
	Name        string `json:"name"`
	StorageKey  string `json:"storage_key"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
}

type storedInput struct {
	Type  string       `json:"type"`
	Value string       `json:"value,omitempty"`
	Files []storedFile `json:"files,omitempty"`
}

// CreateTask queues a browser task for a step, unless no enabled recipe
// can take the target (the caller then keeps it as a manual task). It is
// idempotent per (step, key).
func (s *Service) CreateTask(ctx context.Context, spec TaskSpec) (uuid.UUID, bool, error) {
	var recipe browserdb.BrowserRecipe
	var existing *browserdb.BrowserTask
	err := s.tx(ctx, spec.OrgID, uuid.Nil, func(q *browserdb.Queries, _ pgx.Tx) error {
		tasks, err := q.TasksForStep(ctx, browserdb.TasksForStepParams{OrgID: spec.OrgID, StepID: spec.StepID})
		if err != nil {
			return err
		}
		for i := range tasks {
			if tasks[i].TaskKey == spec.Key {
				existing = &tasks[i]
				return nil
			}
		}
		recipe, err = q.EnabledRecipe(ctx, browserdb.EnabledRecipeParams{CapabilityKey: spec.Capability, Market: spec.Market})
		return err
	})
	if existing != nil {
		return existing.ID, true, nil
	}
	if db.IsNoRows(err) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	rec, verrs := ValidateRecipe([]byte(recipe.RecipeJson))
	if verrs != nil {
		return uuid.Nil, false, fmt.Errorf("browser: enabled recipe %s is invalid: %v", recipe.ID, verrs)
	}
	id := db.NewID()
	inputs, ok, err := s.buildInputs(ctx, spec, id, rec)
	if err != nil || !ok {
		return uuid.Nil, false, err
	}
	in, _ := json.Marshal(inputs)
	pv, _ := json.Marshal(preview(inputs))
	var out uuid.UUID
	err = s.tx(ctx, spec.OrgID, uuid.Nil, func(q *browserdb.Queries, _ pgx.Tx) error {
		t, err := q.InsertTask(ctx, browserdb.InsertTaskParams{ID: id, OrgID: spec.OrgID, ShopConnectionID: spec.ShopID, RunID: spec.RunID,
			StepID: spec.StepID, TaskKey: spec.Key, CapabilityKey: spec.Capability, Market: spec.Market, Title: spec.Title, Input: in, Preview: pv})
		out = t.ID
		return err
	})
	return out, err == nil, err
}

// buildInputs maps the approved target onto the recipe's inputs. Files are
// fetched (HTTPS, public hosts only), sanitized and stored.
func (s *Service) buildInputs(ctx context.Context, spec TaskSpec, taskID uuid.UUID, rec Recipe) (map[string]storedInput, bool, error) {
	values := map[string]any{}
	switch t := spec.Target.(type) {
	case map[string]any:
		values = t
	default:
		// A single value fills the recipe's only input of the matching kind.
		var only string
		for name := range rec.Inputs {
			if only != "" {
				return nil, false, nil
			}
			only = name
		}
		if only != "" {
			values[only] = t
		}
	}
	out := map[string]storedInput{}
	for name, decl := range rec.Inputs {
		v, present := values[name]
		switch decl.Type {
		case "text":
			str, isStr := v.(string)
			if !present || !isStr || str == "" {
				if decl.IsRequired() {
					return nil, false, nil
				}
				continue
			}
			out[name] = storedInput{Type: "text", Value: str}
		case "files":
			urls := toStrings(v)
			if len(urls) == 0 {
				if decl.IsRequired() {
					return nil, false, nil
				}
				continue
			}
			if len(urls) > 10 {
				return nil, false, nil
			}
			var files []storedFile
			for i, u := range urls {
				raw, err := s.Fetcher.Fetch(ctx, u)
				if err == nil && s.Scanner != nil {
					err = s.Scanner.Scan(ctx, raw)
				}
				if err != nil {
					s.Log.WarnContext(ctx, "browser task input not fetched", "task_key", spec.Key, "error", err.Error())
					return nil, false, nil
				}
				img, err := media.Sanitize(raw)
				if err != nil {
					s.Log.WarnContext(ctx, "browser task input rejected", "task_key", spec.Key, "error", err.Error())
					return nil, false, nil
				}
				ext := ".jpg"
				if img.ContentType == "image/png" {
					ext = ".png"
				}
				key := fmt.Sprintf("orgs/%s/browser-tasks/%s/%s-%d%s", spec.OrgID, taskID, name, i, ext)
				if err := s.Store.Put(ctx, key, img.Data, img.ContentType); err != nil {
					return nil, false, err
				}
				base := path.Base(u)
				if i := strings.IndexAny(base, "?#"); i >= 0 {
					base = base[:i]
				}
				files = append(files, storedFile{Name: clean(base, 100), StorageKey: key, ContentType: img.ContentType,
					SHA256: fmt.Sprintf("%x", img.SHA256)})
			}
			out[name] = storedInput{Type: "files", Files: files}
		}
	}
	return out, true, nil
}

func toStrings(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []string:
		return t
	case []any:
		var out []string
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func preview(in map[string]storedInput) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		if v.Type == "text" {
			out[k] = v.Value
		} else {
			names := make([]string, 0, len(v.Files))
			for _, f := range v.Files {
				names = append(names, f.Name)
			}
			out[k] = names
		}
	}
	return out
}

// TaskView is a task as the extension and the dashboard see it.
type TaskView struct {
	ID            uuid.UUID       `json:"id"`
	Status        string          `json:"status"`
	Capability    string          `json:"capability"`
	Title         string          `json:"title"`
	Shop          TaskShop        `json:"shop"`
	Preview       json.RawMessage `json:"preview"`
	RunID         uuid.UUID       `json:"run_id"`
	AttemptCount  int             `json:"attempt_count"`
	StatusMessage *string         `json:"status_message"`
	LastError     json.RawMessage `json:"last_error"`
	StepLog       json.RawMessage `json:"step_log,omitempty"`
	ScreenshotIDs []uuid.UUID     `json:"screenshot_ids,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

// TaskShop identifies the shop a task is for.
type TaskShop struct {
	ID           uuid.UUID `json:"id"`
	ShopeeShopID string    `json:"shopee_shop_id"`
	Name         *string   `json:"name"`
	Region       string    `json:"region"`
}

func rawOr(b []byte, def string) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage(def)
	}
	return b
}

func toView(t browserdb.BrowserTask, shopeeID int64, name *string) TaskView {
	return TaskView{ID: t.ID, Status: t.Status, Capability: t.CapabilityKey, Title: t.Title, RunID: t.RunID,
		Shop:    TaskShop{ID: t.ShopConnectionID, ShopeeShopID: strconv.FormatInt(shopeeID, 10), Name: name, Region: t.Market},
		Preview: rawOr(t.Preview, "{}"), AttemptCount: int(t.AttemptCount), StatusMessage: t.StatusMessage,
		LastError: rawOr(t.LastError, "null"), CreatedAt: t.CreatedAt.UTC(), FinishedAt: t.FinishedAt}
}

// ListForDevice returns the tasks the extension can work on. Tasks whose
// step was finished another way are cancelled here.
func (s *Service) ListForDevice(ctx context.Context, dev Device) ([]TaskView, error) {
	now := s.now()
	out := []TaskView{}
	err := s.tx(ctx, dev.OrgID, dev.UserID, func(q *browserdb.Queries, _ pgx.Tx) error {
		rows, err := q.TasksForDevice(ctx, browserdb.TasksForDeviceParams{OrgID: dev.OrgID, DeviceID: &dev.ID, Now: &now})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.StepStatus != "WAITING_USER" {
				if err := q.CancelStepTasks(ctx, browserdb.CancelStepTasksParams{Now: &now, Message: &msgStepDone, OrgID: dev.OrgID, StepID: r.BrowserTask.StepID}); err != nil {
					return err
				}
				continue
			}
			v := toView(r.BrowserTask, r.ShopeeShopID, r.ShopName)
			if r.BrowserTask.Status != TaskQueued && (r.BrowserTask.LeaseExpiresAt == nil || r.BrowserTask.LeaseExpiresAt.Before(now)) {
				v.Status = TaskQueued
			}
			out = append(out, v)
		}
		return nil
	})
	return out, wrapInternal(err)
}

var msgStepDone = "Bước này của quy trình đã được hoàn tất theo cách khác."

// ClaimResult is what the extension needs to run a task.
type ClaimResult struct {
	LeaseToken     uuid.UUID             `json:"lease_token"`
	LeaseExpiresAt time.Time             `json:"lease_expires_at"`
	Recipe         SignedRecipe          `json:"recipe"`
	Inputs         map[string]ClaimInput `json:"inputs"`
	Task           ClaimTask             `json:"task"`
}

// SignedRecipe carries the exact signed bytes.
type SignedRecipe struct {
	JSON      string `json:"json"`
	Signature string `json:"signature"`
	KeyID     string `json:"key_id"`
}

// ClaimInput is one input value.
type ClaimInput struct {
	Type  string      `json:"type"`
	Value string      `json:"value,omitempty"`
	Files []ClaimFile `json:"files,omitempty"`
}

// ClaimFile is a downloadable input file.
type ClaimFile struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	SHA256      string `json:"sha256"`
}

// ClaimTask identifies the task's shop and capability.
type ClaimTask struct {
	ID           uuid.UUID `json:"id"`
	ShopeeShopID string    `json:"shopee_shop_id"`
	Region       string    `json:"region"`
	Capability   string    `json:"capability"`
}

var errUnavailable = apperr.Conflict("task_unavailable", "Việc này không còn khả dụng.")

// Claim leases a task to a device. Kill switches are checked every time.
func (s *Service) Claim(ctx context.Context, dev Device, id uuid.UUID) (ClaimResult, error) {
	if s.Signer == nil {
		return ClaimResult{}, apperr.Unavailable("recipes_unavailable", "Máy chủ này chưa được cấu hình ký kịch bản thao tác.")
	}
	now := s.now()
	var out ClaimResult
	var inputs map[string]storedInput
	err := s.tx(ctx, dev.OrgID, dev.UserID, func(q *browserdb.Queries, tx pgx.Tx) error {
		t, err := q.LockTask(ctx, browserdb.LockTaskParams{OrgID: dev.OrgID, ID: id})
		if err != nil {
			return apperr.NotFound("Không tìm thấy việc.")
		}
		st, err := q.StepState(ctx, browserdb.StepStateParams{OrgID: dev.OrgID, StepID: t.StepID})
		if err != nil {
			return err
		}
		if st.Status != "WAITING_USER" {
			return errUnavailable // ListForDevice cancels such tasks
		}
		leased := t.Status == TaskRunning || t.Status == TaskWaitingUser
		expired := t.LeaseExpiresAt == nil || !t.LeaseExpiresAt.After(now)
		// Queued tasks, and leased tasks whose lease expired, can be claimed.
		if t.Status != TaskQueued && (!leased || !expired) {
			return errUnavailable
		}
		shop, err := q.ShopRegion(ctx, browserdb.ShopRegionParams{OrgID: dev.OrgID, ID: t.ShopConnectionID})
		if err != nil {
			return err
		}
		if shop.Status != "ACTIVE" {
			return apperr.Conflict("shop_not_active", "Shop chưa được kết nối. Hãy kết nối lại shop trên trang quản trị trước.")
		}
		cs, err := q.CapabilityState(ctx, browserdb.CapabilityStateParams{Market: t.Market, Key: t.CapabilityKey})
		if err != nil {
			return err
		}
		if cs.Enabled == nil || !*cs.Enabled {
			return apperr.Conflict("capability_disabled", "Thao tác này hiện đang bị tắt trên nền tảng.")
		}
		recipe, err := q.EnabledRecipe(ctx, browserdb.EnabledRecipeParams{CapabilityKey: t.CapabilityKey, Market: t.Market})
		if db.IsNoRows(err) {
			return apperr.Conflict("recipe_disabled", "Các bước tự động trên trình duyệt cho thao tác này hiện đang tắt. Hãy làm thủ công rồi đánh dấu việc là đã xong.")
		}
		if err != nil {
			return err
		}
		lease := uuid.New()
		if err := q.ClaimTask(ctx, browserdb.ClaimTaskParams{DeviceID: &dev.ID, LeaseToken: &lease, LeaseExpiresAt: ptr(now.Add(leaseTTL)),
			RecipeID: &recipe.ID, OrgID: dev.OrgID, ID: t.ID}); err != nil {
			return err
		}
		if err := json.Unmarshal(t.Input, &inputs); err != nil {
			return err
		}
		out = ClaimResult{LeaseToken: lease, LeaseExpiresAt: now.Add(leaseTTL),
			Recipe: SignedRecipe{JSON: recipe.RecipeJson, Signature: s.Signer.Sign([]byte(recipe.RecipeJson)), KeyID: s.Signer.KeyID},
			Task:   ClaimTask{ID: t.ID, ShopeeShopID: strconv.FormatInt(shop.ShopeeShopID, 10), Region: t.Market, Capability: t.CapabilityKey}}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &dev.OrgID, ActorUserID: &dev.UserID, Action: "browser_task.claimed",
			TargetType: "browser_task", TargetID: &t.ID, Metadata: map[string]any{"device_id": dev.ID.String(), "recipe_version": recipe.Version}})
	})
	if err != nil {
		return ClaimResult{}, wrapInternal(err)
	}
	out.Inputs = map[string]ClaimInput{}
	for name, in := range inputs {
		ci := ClaimInput{Type: in.Type, Value: in.Value}
		for _, f := range in.Files {
			u, err := s.Store.PresignGet(ctx, f.StorageKey, inputURLTTL, f.Name)
			if err != nil {
				return ClaimResult{}, apperr.Internal(err)
			}
			ci.Files = append(ci.Files, ClaimFile{Name: f.Name, URL: u, ContentType: f.ContentType, SHA256: f.SHA256})
		}
		out.Inputs[name] = ci
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

// HeartbeatInput extends a lease and reports a pause.
type HeartbeatInput struct {
	LeaseToken uuid.UUID `json:"lease_token"`
	State      string    `json:"state"`
	Message    *string   `json:"message"`
}

var errLeaseLost = apperr.Conflict("lease_lost", "Một trình duyệt khác đã nhận việc này, hoặc việc đã bị hủy.")

// Heartbeat extends the lease.
func (s *Service) Heartbeat(ctx context.Context, dev Device, id uuid.UUID, in HeartbeatInput) error {
	if in.State != TaskRunning && in.State != TaskWaitingUser {
		return apperr.Invalid(apperr.FieldError{Field: "state", Code: "invalid", Message: "Phải là RUNNING hoặc WAITING_USER."})
	}
	var msg *string
	if in.Message != nil {
		m := clean(*in.Message, 500)
		msg = &m
	}
	err := s.tx(ctx, dev.OrgID, dev.UserID, func(q *browserdb.Queries, _ pgx.Tx) error {
		n, err := q.HeartbeatTask(ctx, browserdb.HeartbeatTaskParams{Status: in.State, LeaseExpiresAt: ptr(s.now().Add(leaseTTL)),
			StatusMessage: msg, OrgID: dev.OrgID, ID: id, LeaseToken: &in.LeaseToken})
		if err == nil && n == 0 {
			return errLeaseLost
		}
		return err
	})
	return wrapInternal(err)
}

// ReleaseInput gives a task back without a result (the seller pressed Stop).
type ReleaseInput struct {
	LeaseToken uuid.UUID `json:"lease_token"`
}

// Release returns a held task to the queue. It does not count as a run of
// the recipe.
func (s *Service) Release(ctx context.Context, dev Device, id uuid.UUID, in ReleaseInput) error {
	msg := "Đã dừng trên trình duyệt; sẵn sàng chạy lại."
	err := s.tx(ctx, dev.OrgID, dev.UserID, func(q *browserdb.Queries, tx pgx.Tx) error {
		n, err := q.ReleaseTask(ctx, browserdb.ReleaseTaskParams{StatusMessage: &msg, OrgID: dev.OrgID, ID: id, LeaseToken: &in.LeaseToken})
		if err != nil {
			return err
		}
		if n == 0 {
			return errLeaseLost
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &dev.OrgID, ActorUserID: &dev.UserID, Action: "browser_task.released",
			TargetType: "browser_task", TargetID: &id, Metadata: map[string]any{"device_id": dev.ID.String()}})
	})
	return wrapInternal(err)
}

// AddScreenshot stores a screenshot of the Seller Centre tab.
func (s *Service) AddScreenshot(ctx context.Context, dev Device, id, lease uuid.UUID, data []byte) (uuid.UUID, error) {
	if len(data) > maxScreenshotSize {
		return uuid.Nil, &apperr.Error{Kind: apperr.KindPayloadTooLarge, Code: "payload_too_large", Message: "Ảnh chụp màn hình tối đa 5 MB."}
	}
	img, err := media.Sanitize(data)
	if err != nil {
		return uuid.Nil, apperr.Invalid(apperr.FieldError{Field: "file", Code: "invalid_image", Message: "Hãy gửi ảnh chụp màn hình định dạng PNG hoặc JPEG."})
	}
	sid := db.NewID()
	ext := ".png"
	if img.ContentType == "image/jpeg" {
		ext = ".jpg"
	}
	key := fmt.Sprintf("orgs/%s/browser-tasks/%s/screenshots/%s%s", dev.OrgID, id, sid, ext)
	err = s.tx(ctx, dev.OrgID, dev.UserID, func(q *browserdb.Queries, _ pgx.Tx) error {
		t, err := q.LockTask(ctx, browserdb.LockTaskParams{OrgID: dev.OrgID, ID: id})
		if err != nil {
			return apperr.NotFound("Không tìm thấy việc.")
		}
		if t.LeaseToken == nil || *t.LeaseToken != lease {
			return errLeaseLost
		}
		n, err := q.CountScreenshots(ctx, browserdb.CountScreenshotsParams{OrgID: dev.OrgID, TaskID: id})
		if err != nil {
			return err
		}
		if n >= maxScreenshots {
			return apperr.Conflict("too_many_screenshots", "Việc này đã có đủ 10 ảnh chụp màn hình.")
		}
		if err := s.Store.Put(ctx, key, img.Data, img.ContentType); err != nil {
			return err
		}
		return q.InsertScreenshot(ctx, browserdb.InsertScreenshotParams{ID: sid, OrgID: dev.OrgID, TaskID: id, StorageKey: key})
	})
	return sid, wrapInternal(err)
}

// StepLog is one entry of the extension's run log.
type StepLog struct {
	Index      int     `json:"index"`
	Action     string  `json:"action"`
	Outcome    string  `json:"outcome"`
	DurationMs int     `json:"duration_ms"`
	Error      *string `json:"error,omitempty"`
}

// ResultInput finishes a task.
type ResultInput struct {
	LeaseToken uuid.UUID `json:"lease_token"`
	Status     string    `json:"status"`
	ErrorCode  *string   `json:"error_code"`
	Message    *string   `json:"message"`
	Steps      []StepLog `json:"steps"`
	// ScreenshotIDs repeats the screenshots uploaded for this run; they are
	// already linked to the task, so the list is informational.
	ScreenshotIDs []uuid.UUID `json:"screenshot_ids"`
}

// Result records the outcome. When every browser task of the step has
// succeeded and the step has no other manual task, the step completes.
func (s *Service) Result(ctx context.Context, dev Device, id uuid.UUID, in ResultInput) error {
	if in.Status != TaskSucceeded && in.Status != TaskFailed {
		return apperr.Invalid(apperr.FieldError{Field: "status", Code: "invalid", Message: "Phải là SUCCEEDED hoặc FAILED."})
	}
	if len(in.Steps) > maxLogEntries {
		in.Steps = in.Steps[:maxLogEntries]
	}
	for i := range in.Steps {
		in.Steps[i].Action = clean(in.Steps[i].Action, 40)
		in.Steps[i].Outcome = clean(in.Steps[i].Outcome, 20)
		if in.Steps[i].Error != nil {
			e := clean(*in.Steps[i].Error, 500)
			in.Steps[i].Error = &e
		}
	}
	logJSON, _ := json.Marshal(in.Steps)
	if in.Steps == nil {
		logJSON = []byte("[]")
	}
	var lastErr []byte
	var msg *string
	if in.Message != nil {
		m := clean(*in.Message, 500)
		msg = &m
	}
	if in.Status == TaskFailed {
		code := "failed"
		if in.ErrorCode != nil {
			code = clean(*in.ErrorCode, 60)
		}
		lastErr, _ = json.Marshal(map[string]any{"code": code, "message": msg})
	}
	now := s.now()
	var task browserdb.BrowserTask
	var succeeded []uuid.UUID
	err := s.tx(ctx, dev.OrgID, dev.UserID, func(q *browserdb.Queries, tx pgx.Tx) error {
		t, err := q.LockTask(ctx, browserdb.LockTaskParams{OrgID: dev.OrgID, ID: id})
		if err != nil {
			return apperr.NotFound("Không tìm thấy việc.")
		}
		n, err := q.FinishTask(ctx, browserdb.FinishTaskParams{Status: in.Status, LastError: lastErr, StepLog: logJSON, StatusMessage: msg,
			FinishedAt: &now, OrgID: dev.OrgID, ID: id, LeaseToken: &in.LeaseToken})
		if err != nil {
			return err
		}
		if n == 0 {
			return errLeaseLost
		}
		task = t
		if t.RecipeID != nil && (in.Status == TaskSucceeded || recipeFault(in.ErrorCode)) {
			if in.Status == TaskSucceeded {
				err = q.RecordRecipeSuccess(ctx, *t.RecipeID)
			} else {
				var r browserdb.RecordRecipeFailureRow
				limit := int32(s.autoDisable()) // #nosec G115 -- autoDisable is < 1000
				r, err = q.RecordRecipeFailure(ctx, browserdb.RecordRecipeFailureParams{AutoDisableAfter: limit, ID: *t.RecipeID})
				if err == nil && !r.Enabled && int(r.ConsecutiveFailures) == s.autoDisable() {
					s.Log.WarnContext(ctx, "browser recipe disabled after repeated failures", "recipe_id", t.RecipeID.String())
				}
			}
			if err != nil {
				return err
			}
		}
		if in.Status == TaskSucceeded {
			all, err := q.TasksForStep(ctx, browserdb.TasksForStepParams{OrgID: dev.OrgID, StepID: t.StepID})
			if err != nil {
				return err
			}
			for _, x := range all {
				if x.Status == TaskSucceeded {
					succeeded = append(succeeded, x.ID)
				}
			}
		}
		action := "browser_task.succeeded"
		if in.Status == TaskFailed {
			action = "browser_task.failed"
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &dev.OrgID, ActorUserID: &dev.UserID, Action: action,
			TargetType: "browser_task", TargetID: &id, Metadata: map[string]any{"device_id": dev.ID.String()}})
	})
	if err != nil {
		return wrapInternal(err)
	}
	if in.Status == TaskSucceeded && s.Steps != nil {
		if err := s.Steps.CompleteFromExtension(ctx, dev.Principal(), dev.Tenant(), task.RunID, task.StepID, succeeded); err != nil {
			// The task result is saved; the user can still finish the step.
			s.Log.WarnContext(ctx, "browser task: step not completed", "step_id", task.StepID.String(), "error", err.Error())
		}
	}
	return nil
}

// recipeFault reports whether a failure points at the recipe (Seller
// Centre changed) rather than at the seller or the environment: a seller
// pressing Stop, the wrong shop logged in or a closed tab must not count
// toward disabling the recipe.
func recipeFault(code *string) bool {
	if code == nil {
		return true
	}
	switch *code {
	case "stopped_by_user", "shop_mismatch", "tab_closed", "input_fetch_failed", "input_checksum_mismatch", "missing_input",
		"origin_not_permitted", "unknown_key", "signature_invalid", "navigation_interrupted":
		return false
	}
	return true
}

func (s *Service) autoDisable() int {
	if s.AutoDisableAfter > 0 && s.AutoDisableAfter < 1000 {
		return s.AutoDisableAfter
	}
	return 3
}

// ListForRun returns the browser tasks of a run (dashboard).
func (s *Service) ListForRun(ctx context.Context, p authctx.Principal, t authctx.Tenant, runID uuid.UUID) ([]TaskView, error) {
	out := []TaskView{}
	err := s.tx(ctx, t.OrgID, p.UserID, func(q *browserdb.Queries, _ pgx.Tx) error {
		if ok, err := q.RunExists(ctx, browserdb.RunExistsParams{OrgID: t.OrgID, ID: runID}); err != nil || !ok {
			if err == nil {
				err = apperr.NotFound("Không tìm thấy lượt chạy.")
			}
			return err
		}
		rows, err := q.TasksForRun(ctx, browserdb.TasksForRunParams{OrgID: t.OrgID, RunID: runID})
		if err != nil || len(rows) == 0 {
			return err
		}
		ids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		shots, err := q.ScreenshotsForTasks(ctx, browserdb.ScreenshotsForTasksParams{OrgID: t.OrgID, TaskIds: ids})
		if err != nil {
			return err
		}
		shop, err := q.ShopRegion(ctx, browserdb.ShopRegionParams{OrgID: t.OrgID, ID: rows[0].ShopConnectionID})
		if err != nil {
			return err
		}
		for _, r := range rows {
			v := toView(r, shop.ShopeeShopID, shop.ShopName)
			v.StepLog = rawOr(r.StepLog, "[]")
			for _, sh := range shots {
				if sh.TaskID == r.ID {
					v.ScreenshotIDs = append(v.ScreenshotIDs, sh.ID)
				}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, wrapInternal(err)
}

// Requeue puts a failed task back in the queue (dashboard).
func (s *Service) Requeue(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) error {
	err := s.tx(ctx, t.OrgID, p.UserID, func(q *browserdb.Queries, tx pgx.Tx) error {
		task, err := q.LockTask(ctx, browserdb.LockTaskParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return apperr.NotFound("Không tìm thấy việc.")
		}
		st, err := q.StepState(ctx, browserdb.StepStateParams{OrgID: t.OrgID, StepID: task.StepID})
		if err != nil {
			return err
		}
		if task.Status != TaskFailed || st.Status != "WAITING_USER" {
			return apperr.Conflict("not_retryable", "Chỉ có thể chạy lại việc bị lỗi của một bước đang chờ.")
		}
		if err := q.SetTaskStatus(ctx, browserdb.SetTaskStatusParams{Status: TaskQueued, OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "browser_task.requeued",
			TargetType: "browser_task", TargetID: &id})
	})
	return wrapInternal(err)
}

// ScreenshotURL returns a short-lived link to a screenshot (dashboard).
func (s *Service) ScreenshotURL(ctx context.Context, p authctx.Principal, t authctx.Tenant, taskID, shotID uuid.UUID) (string, time.Time, error) {
	var key string
	err := s.tx(ctx, t.OrgID, p.UserID, func(q *browserdb.Queries, tx pgx.Tx) error {
		sh, err := q.GetScreenshot(ctx, browserdb.GetScreenshotParams{OrgID: t.OrgID, TaskID: taskID, ID: shotID})
		if err != nil {
			return apperr.NotFound("Không tìm thấy ảnh chụp màn hình.")
		}
		key = sh.StorageKey
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "browser_task.screenshot_viewed",
			TargetType: "browser_task", TargetID: &taskID})
	})
	if err != nil {
		return "", time.Time{}, wrapInternal(err)
	}
	ttl := 5 * time.Minute
	u, err := s.Store.PresignGet(ctx, key, ttl, "screenshot"+path.Ext(key))
	if err != nil {
		return "", time.Time{}, apperr.Internal(err)
	}
	return u, s.now().Add(ttl), nil
}
