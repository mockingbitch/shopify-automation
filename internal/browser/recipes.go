package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/browser/browserdb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
)

// RecipeView is a recipe as platform admins see it.
type RecipeView struct {
	ID                  uuid.UUID       `json:"id"`
	Capability          string          `json:"capability"`
	Market              string          `json:"market"`
	Version             int             `json:"version"`
	Recipe              json.RawMessage `json:"recipe"`
	Enabled             bool            `json:"enabled"`
	DisabledReason      *string         `json:"disabled_reason"`
	VerifiedAt          *time.Time      `json:"verified_at"`
	Notes               string          `json:"notes"`
	SuccessCount        int             `json:"success_count"`
	FailureCount        int             `json:"failure_count"`
	ConsecutiveFailures int             `json:"consecutive_failures"`
	CreatedAt           time.Time       `json:"created_at"`
}

func toRecipe(r browserdb.BrowserRecipe) RecipeView {
	return RecipeView{ID: r.ID, Capability: r.CapabilityKey, Market: r.Market, Version: int(r.Version), Recipe: json.RawMessage(r.RecipeJson),
		Enabled: r.Enabled, DisabledReason: r.DisabledReason, VerifiedAt: r.VerifiedAt, Notes: r.Notes, SuccessCount: int(r.SuccessCount),
		FailureCount: int(r.FailureCount), ConsecutiveFailures: int(r.ConsecutiveFailures), CreatedAt: r.CreatedAt.UTC()}
}

func (s *Service) adminTx(ctx context.Context, p authctx.Principal, fn func(q *browserdb.Queries, tx pgx.Tx) error) error {
	return s.DB.Tx(ctx, db.Scope{UserID: p.UserID}, func(tx pgx.Tx) error { return fn(browserdb.New(tx), tx) })
}

// ListRecipes lists every recipe version.
func (s *Service) ListRecipes(ctx context.Context, p authctx.Principal) ([]RecipeView, error) {
	out := []RecipeView{}
	err := s.adminTx(ctx, p, func(q *browserdb.Queries, _ pgx.Tx) error {
		rows, err := q.ListRecipes(ctx)
		for _, r := range rows {
			out = append(out, toRecipe(r))
		}
		return err
	})
	return out, wrapInternal(err)
}

// CreateRecipeInput adds a recipe version (disabled and unverified).
type CreateRecipeInput struct {
	Recipe json.RawMessage `json:"recipe"`
	Notes  string          `json:"notes"`
}

// CreateRecipe stores a new version. The text is kept byte for byte,
// because those bytes are what the extension verifies.
func (s *Service) CreateRecipe(ctx context.Context, p authctx.Principal, in CreateRecipeInput) (RecipeView, error) {
	if len(in.Recipe) == 0 || len(in.Recipe) > 65536 {
		return RecipeView{}, apperr.Invalid(apperr.FieldError{Field: "recipe", Code: "invalid", Message: "Hãy gửi kịch bản thao tác dưới dạng đối tượng JSON (tối đa 64 KB)."})
	}
	rec, errs := ValidateRecipe(in.Recipe)
	if errs != nil {
		return RecipeView{}, apperr.Invalid(errs...)
	}
	var out RecipeView
	err := s.adminTx(ctx, p, func(q *browserdb.Queries, tx pgx.Tx) error {
		cs, err := q.CapabilityState(ctx, browserdb.CapabilityStateParams{Market: rec.Market, Key: rec.Capability})
		if db.IsNoRows(err) {
			return apperr.Invalid(apperr.FieldError{Field: "recipe.capability", Code: "unknown", Message: "Không có chức năng này."})
		}
		if err != nil {
			return err
		}
		if cs.DefaultMode != "BROWSER" {
			return apperr.Invalid(apperr.FieldError{Field: "recipe.capability", Code: "not_browser",
				Message: "Chỉ những chức năng không có Open API (chế độ BROWSER) mới có kịch bản thao tác."})
		}
		next, err := q.NextRecipeVersion(ctx, browserdb.NextRecipeVersionParams{CapabilityKey: rec.Capability, Market: rec.Market})
		if err != nil {
			return err
		}
		if int(next) != rec.Version {
			return apperr.Invalid(apperr.FieldError{Field: "recipe.version", Code: "version",
				Message: fmt.Sprintf("Phiên bản tiếp theo của %s tại %s phải là %d.", rec.Capability, rec.Market, next)})
		}
		r, err := q.InsertRecipe(ctx, browserdb.InsertRecipeParams{ID: db.NewID(), CapabilityKey: rec.Capability, Market: rec.Market,
			Version: next, RecipeJson: string(in.Recipe), Notes: clean(in.Notes, 2000), CreatedBy: &p.UserID})
		if err != nil {
			return err
		}
		out = toRecipe(r)
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "browser_recipe.created", TargetType: "browser_recipe",
			TargetID: &r.ID, Metadata: map[string]any{"capability": rec.Capability, "market": rec.Market, "version": rec.Version}})
	})
	return out, wrapInternal(err)
}

// UpdateRecipeInput changes verification and the kill switch.
type UpdateRecipeInput struct {
	Verified *bool   `json:"verified"`
	Enabled  *bool   `json:"enabled"`
	Notes    *string `json:"notes"`
}

// UpdateRecipe marks a version verified (after a run on a real seller
// account) and enables or disables it. Enabling one version disables the
// others of the same capability and market.
func (s *Service) UpdateRecipe(ctx context.Context, p authctx.Principal, id uuid.UUID, in UpdateRecipeInput) (RecipeView, error) {
	var out RecipeView
	err := s.adminTx(ctx, p, func(q *browserdb.Queries, tx pgx.Tx) error {
		r, err := q.LockRecipe(ctx, id)
		if err != nil {
			return apperr.NotFound("Không tìm thấy kịch bản thao tác.")
		}
		verifiedAt, verifiedBy, enabled, reason, notes := r.VerifiedAt, r.VerifiedBy, r.Enabled, r.DisabledReason, r.Notes
		if in.Notes != nil {
			notes = clean(*in.Notes, 2000)
		}
		if in.Verified != nil {
			if *in.Verified {
				now := s.now()
				verifiedAt, verifiedBy = &now, &p.UserID
			} else {
				verifiedAt, verifiedBy, enabled = nil, nil, false
				m := "Đã bỏ xác minh"
				reason = &m
			}
		}
		if in.Enabled != nil {
			if *in.Enabled && verifiedAt == nil {
				return apperr.Conflict("not_verified", "Hãy kiểm tra kịch bản thao tác trên một tài khoản người bán thật trước khi bật.")
			}
			enabled = *in.Enabled
			if enabled {
				reason = nil
			} else {
				m := "Đã bị quản trị nền tảng tắt"
				reason = &m
			}
		}
		if enabled {
			if err := q.DisableOtherVersions(ctx, browserdb.DisableOtherVersionsParams{Version: r.Version, CapabilityKey: r.CapabilityKey,
				Market: r.Market, ID: r.ID}); err != nil {
				return err
			}
		}
		r, err = q.SetRecipeState(ctx, browserdb.SetRecipeStateParams{Enabled: enabled, DisabledReason: reason, VerifiedAt: verifiedAt,
			VerifiedBy: verifiedBy, Notes: notes, ID: id})
		if err != nil {
			return err
		}
		out = toRecipe(r)
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "browser_recipe.updated", TargetType: "browser_recipe",
			TargetID: &id, Metadata: map[string]any{"enabled": enabled, "verified": verifiedAt != nil}})
	})
	return out, wrapInternal(err)
}
