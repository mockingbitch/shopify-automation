package shopsetup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/httpx"
	"shopee-automation/internal/shops"
	"shopee-automation/internal/templates"
	"shopee-automation/internal/workflow"
	"shopee-automation/internal/workflow/workflowdb"
)

const maxApplyShops = 50

// ApplyInput lists the shops to configure.
type ApplyInput struct {
	ShopIDs []uuid.UUID `json:"shop_ids"`
}

// ApplyResult is returned when runs were started.
type ApplyResult struct {
	BatchID uuid.UUID   `json:"batch_id"`
	RunIDs  []uuid.UUID `json:"workflow_run_ids"`
	Replay  bool        `json:"replayed"`
}

// Apply starts one SHOP_SETUP run per shop for a published version. With an
// idempotency key, repeating the same request returns the same runs and a
// different request with the same key is refused.
func (s *Setup) Apply(ctx context.Context, p authctx.Principal, t authctx.Tenant, templateID, versionID uuid.UUID,
	in ApplyInput, idemKey string, rec audit.Recorder) (ApplyResult, error) {
	if len(in.ShopIDs) == 0 || len(in.ShopIDs) > maxApplyShops {
		return ApplyResult{}, apperr.Invalid(apperr.FieldError{Field: "shop_ids", Code: "invalid", Message: "Chọn từ 1 đến 50 shop."})
	}
	ids := append([]uuid.UUID(nil), in.ShopIDs...)
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			return ApplyResult{}, apperr.Invalid(apperr.FieldError{Field: "shop_ids", Code: "duplicate", Message: "Mỗi shop chỉ được chọn một lần."})
		}
	}
	var key *string
	if idemKey != "" {
		if _, err := uuid.Parse(idemKey); err != nil {
			return ApplyResult{}, apperr.BadRequest("bad_request", "Idempotency-Key phải là UUID hợp lệ.")
		}
		key = &idemKey
	}
	h := sha256.New()
	h.Write([]byte(templateID.String() + "|" + versionID.String()))
	for _, id := range ids {
		h.Write([]byte("|" + id.String()))
	}
	hash := h.Sum(nil)

	// A repeated key is answered before anything else is checked.
	if key != nil {
		var replay *ApplyResult
		err := s.Engine.DB.Tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(tx pgx.Tx) error {
			var err error
			replay, err = replayFor(ctx, workflowdb.New(tx), t.OrgID, *key, hash)
			return err
		})
		if err != nil {
			return ApplyResult{}, wrapErr(err)
		}
		if replay != nil {
			return *replay, nil
		}
	}

	status, versionNo, name, err := s.Templates.VersionStatus(ctx, t, templateID, versionID)
	if err != nil {
		return ApplyResult{}, err
	}
	if status != templates.StatusPublished {
		return ApplyResult{}, apperr.Conflict("version_not_published", "Chỉ có thể áp dụng phiên bản đã phát hành của mẫu shop.")
	}
	var inactive []string
	for _, id := range ids {
		d, err := s.Shops.Get(ctx, t, id)
		if err != nil {
			if apperr.IsKind(err, apperr.KindNotFound) {
				return ApplyResult{}, apperr.Invalid(apperr.FieldError{Field: "shop_ids", Code: "not_found", Message: "Không tìm thấy shop " + id.String() + "."})
			}
			return ApplyResult{}, err
		}
		if d.Shop.Status != shops.StatusActive {
			name := d.Shop.ShopeeShopID
			if d.Shop.ShopName != nil {
				name = *d.Shop.ShopName
			}
			inactive = append(inactive, name)
		}
	}
	if len(inactive) > 0 {
		return ApplyResult{}, apperr.FailedDependency("shop_reconnect_required", "Các shop sau chưa được kết nối: "+strings.Join(inactive, ", ")+". Hãy kết nối lại rồi thử lại.")
	}

	var out ApplyResult
	var runIDs []uuid.UUID
	err = s.Engine.DB.Tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(tx pgx.Tx) error {
		q := workflowdb.New(tx)
		if key != nil {
			if err := q.AdvisoryLock(ctx, "apply:"+t.OrgID.String()+":"+*key); err != nil {
				return err
			}
			replay, err := replayFor(ctx, q, t.OrgID, *key, hash)
			if err != nil {
				return err
			}
			if replay != nil {
				out = *replay
				return nil
			}
		}
		batch := db.NewID()
		specs := make([]workflow.StartSpec, 0, len(in.ShopIDs))
		for _, id := range in.ShopIDs {
			specs = append(specs, workflow.StartSpec{Definition: Key, ShopID: id,
				Input: Input{TemplateID: templateID, VersionID: versionID, TemplateName: name, VersionNo: versionNo}})
		}
		runs, err := s.Engine.CreateRuns(ctx, tx, workflow.StartOptions{OrgID: t.OrgID, UserID: p.UserID, BatchID: &batch,
			IdempotencyKey: key, RequestHash: hash}, specs)
		if err != nil {
			if db.UniqueViolation(err) == "workflow_runs_one_active_setup" {
				return apperr.Conflict("shop_busy", "Một trong các shop này đang có quy trình thiết lập chạy. Hãy chờ xong hoặc hủy nó.")
			}
			return err
		}
		out.BatchID = batch
		for _, r := range runs {
			out.RunIDs = append(out.RunIDs, r.ID)
			runIDs = append(runIDs, r.ID)
		}
		return rec.Record(ctx, tx, auditEntry(p, t, templateID, versionID, batch, len(runs)))
	})
	if err != nil {
		return ApplyResult{}, wrapErr(err)
	}
	s.Engine.Kick(ctx, t.OrgID, runIDs...)
	return out, nil
}

// replayFor returns the earlier result for an idempotency key, or an error
// when the key was used for a different request.
func replayFor(ctx context.Context, q *workflowdb.Queries, orgID uuid.UUID, key string, hash []byte) (*ApplyResult, error) {
	prev, err := q.RunsByIdempotencyKey(ctx, workflowdb.RunsByIdempotencyKeyParams{OrgID: orgID, IdempotencyKey: &key})
	if err != nil || len(prev) == 0 {
		return nil, err
	}
	if !bytes.Equal(prev[0].RequestHash, hash) {
		return nil, apperr.Conflict("idempotency_mismatch", "Idempotency-Key này đã được dùng cho một yêu cầu khác.")
	}
	out := &ApplyResult{Replay: true}
	for _, r := range prev {
		out.RunIDs = append(out.RunIDs, r.ID)
		if r.BatchID != nil {
			out.BatchID = *r.BatchID
		}
	}
	return out, nil
}

func wrapErr(err error) error {
	if _, ok := apperr.As(err); ok {
		return err
	}
	return apperr.Internal(err)
}

func auditEntry(p authctx.Principal, t authctx.Tenant, templateID, versionID, batch uuid.UUID, n int) audit.Entry {
	return audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template.applied", TargetType: "shop_template",
		TargetID: &templateID, Metadata: map[string]any{"version_id": versionID.String(), "batch_id": batch.String(), "shops": n}}
}

// Handler serves the apply endpoint.
type Handler struct {
	Setup *Setup
	Audit audit.Recorder
}

// Apply handles POST /orgs/{orgID}/templates/{templateID}/versions/{versionID}/apply.
func (h Handler) Apply(w http.ResponseWriter, r *http.Request) error {
	tid, err1 := uuid.Parse(chi.URLParam(r, "templateID"))
	vid, err2 := uuid.Parse(chi.URLParam(r, "versionID"))
	if err1 != nil || err2 != nil {
		return apperr.NotFound("Không tìm thấy phiên bản của mẫu shop.")
	}
	var in ApplyInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	out, err := h.Setup.Apply(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), tid, vid, in,
		r.Header.Get("Idempotency-Key"), h.Audit)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[ApplyResult]{Data: out})
}
