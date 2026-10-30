package shops

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/capability"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/shops/shopsdb"
)

// AppOption is an enabled Shopee app a member can connect a shop with.
type AppOption struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Environment string    `json:"environment"`
}

// AppOptions lists enabled apps (no secrets).
func (s *Service) AppOptions(ctx context.Context, t authctx.Tenant) ([]AppOption, error) {
	out := []AppOption{}
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		apps, err := q.ListEnabledApps(ctx)
		for _, a := range apps {
			out = append(out, AppOption{ID: a.ID, Name: a.Name, Environment: a.Environment})
		}
		return err
	})
	return out, wrap(err, nil)
}

// CapabilityDef is a registry entry with its overrides (platform admin).
type CapabilityDef struct {
	Key          string     `json:"key"`
	Title        string     `json:"title"`
	DefaultMode  string     `json:"default_mode"`
	FallbackMode *string    `json:"fallback_mode"`
	Enabled      bool       `json:"enabled"`
	Verification string     `json:"verification"`
	Overrides    []Override `json:"overrides"`
}

// Override is a capability override.
type Override struct {
	ID          uuid.UUID  `json:"id"`
	ShopeeAppID *uuid.UUID `json:"shopee_app_id"`
	Region      *string    `json:"region"`
	Mode        *string    `json:"mode"`
	Enabled     *bool      `json:"enabled"`
	Reason      string     `json:"reason"`
	CreatedAt   time.Time  `json:"created_at"`
}

// OverrideInput creates or replaces the override of one scope.
type OverrideInput struct {
	CapabilityKey string     `json:"capability_key"`
	ShopeeAppID   *uuid.UUID `json:"shopee_app_id"`
	Region        *string    `json:"region"`
	Mode          *string    `json:"mode"`
	Enabled       *bool      `json:"enabled"`
	Reason        string     `json:"reason"`
}

var regions = []string{"VN", "SG", "MY", "TH", "PH", "ID", "TW"}

// Registry lists capability definitions with their overrides.
func (s *Service) Registry(ctx context.Context, p authctx.Principal) ([]CapabilityDef, error) {
	if err := requireAdmin(p); err != nil {
		return nil, err
	}
	var out []CapabilityDef
	err := s.tx(ctx, db.Scope{UserID: p.UserID}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		defs, err := q.ListCapabilityDefinitions(ctx)
		if err != nil {
			return err
		}
		ovs, err := q.ListAllOverrides(ctx)
		if err != nil {
			return err
		}
		for _, d := range defs {
			c := CapabilityDef{Key: d.Key, Title: d.Title, DefaultMode: d.DefaultMode, FallbackMode: d.FallbackMode, Enabled: d.Enabled,
				Verification: d.Verification, Overrides: []Override{}}
			for _, o := range ovs {
				if o.CapabilityKey == d.Key {
					c.Overrides = append(c.Overrides, Override{ID: o.ID, ShopeeAppID: o.ShopeeAppID, Region: o.Region, Mode: o.Mode,
						Enabled: o.Enabled, Reason: o.Reason, CreatedAt: o.CreatedAt.UTC()})
				}
			}
			out = append(out, c)
		}
		return nil
	})
	return out, wrap(err, nil)
}

// PutOverride creates or replaces an override (a kill switch when
// enabled is false).
func (s *Service) PutOverride(ctx context.Context, p authctx.Principal, in OverrideInput) (Override, error) {
	if err := requireAdmin(p); err != nil {
		return Override{}, err
	}
	var errs []apperr.FieldError
	if in.Region != nil && !slices.Contains(regions, *in.Region) {
		errs = append(errs, apperr.FieldError{Field: "region", Code: "invalid", Message: "Unknown market."})
	}
	if in.Mode != nil && !slices.Contains([]string{string(capability.ModeAPI), string(capability.ModeBrowser), string(capability.ModeManual),
		string(capability.ModeUnsupported)}, *in.Mode) {
		errs = append(errs, apperr.FieldError{Field: "mode", Code: "invalid", Message: "Unknown mode."})
	}
	if in.Mode == nil && in.Enabled == nil {
		errs = append(errs, apperr.FieldError{Field: "enabled", Code: "required", Message: "Set enabled or mode."})
	}
	if l := len([]rune(in.Reason)); l < 3 || l > 300 {
		errs = append(errs, apperr.FieldError{Field: "reason", Code: "invalid", Message: "Explain the override (3 to 300 characters)."})
	}
	if len(errs) > 0 {
		return Override{}, apperr.Invalid(errs...)
	}
	var out Override
	err := s.tx(ctx, db.Scope{UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		o, err := q.UpsertOverride(ctx, shopsdb.UpsertOverrideParams{ID: db.NewID(), CapabilityKey: in.CapabilityKey, ShopeeAppID: in.ShopeeAppID,
			Region: in.Region, Mode: in.Mode, Enabled: in.Enabled, Reason: in.Reason})
		if err != nil {
			if db.IsForeignKeyViolation(err) {
				return apperr.Invalid(apperr.FieldError{Field: "capability_key", Code: "unknown", Message: "Unknown capability or Shopee app."})
			}
			return err
		}
		out = Override{ID: o.ID, ShopeeAppID: o.ShopeeAppID, Region: o.Region, Mode: o.Mode, Enabled: o.Enabled, Reason: o.Reason, CreatedAt: o.CreatedAt.UTC()}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "capability.override_set", TargetType: "capability_override",
			TargetID: &o.ID, Metadata: map[string]any{"capability": in.CapabilityKey, "enabled": in.Enabled, "mode": in.Mode, "reason": in.Reason}})
	})
	return out, wrap(err, nil)
}

// DeleteOverride removes an override.
func (s *Service) DeleteOverride(ctx context.Context, p authctx.Principal, id uuid.UUID) error {
	if err := requireAdmin(p); err != nil {
		return err
	}
	err := s.tx(ctx, db.Scope{UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		n, err := q.DeleteOverride(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound("Override not found.")
		}
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "capability.override_removed", TargetType: "capability_override", TargetID: &id})
	})
	return wrap(err, nil)
}
