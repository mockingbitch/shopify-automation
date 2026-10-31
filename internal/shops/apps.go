package shops

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/validate"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops/shopsdb"
)

// Default hosts per environment, from docs/shopee-integration.md §2
// (Sandbox Testing V2 guide and API calls guide, read 2026-10-30). SG-deployed
// servers use the SG production host for every market.
var defaultHosts = map[string]struct{ API, Auth string }{
	"sandbox":    {"https://openplatform.sandbox.test-stable.shopee.sg", "https://open.sandbox.test-stable.shopee.com"},
	"production": {"https://partner.shopeemobile.com", "https://open.shopee.com"},
}

const appsTable = "shopee_apps"

// App is the API view of a partner app. The partner key is write-only.
type App struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Environment     string    `json:"environment"`
	APIHost         string    `json:"api_host"`
	AuthHost        string    `json:"auth_host"`
	PartnerID       string    `json:"partner_id"`
	AppType         string    `json:"app_type"`
	RedirectURI     string    `json:"redirect_uri"`
	PushCallbackURL *string   `json:"push_callback_url"`
	Enabled         bool      `json:"enabled"`
	PartnerKeySet   bool      `json:"partner_key_set"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toApp(a shopsdb.ShopeeApp) App {
	return App{ID: a.ID, Name: a.Name, Environment: a.Environment, APIHost: a.ApiHost, AuthHost: a.AuthHost,
		PartnerID: strconv.FormatInt(a.PartnerID, 10), AppType: a.AppType, RedirectURI: a.RedirectUri,
		PushCallbackURL: a.PushCallbackUrl, Enabled: a.Enabled, PartnerKeySet: len(a.PartnerKeyEnc) > 0,
		CreatedAt: a.CreatedAt.UTC(), UpdatedAt: a.UpdatedAt.UTC()}
}

// CreateAppInput registers a partner app.
type CreateAppInput struct {
	Name            string  `json:"name"`
	Environment     string  `json:"environment"`
	PartnerID       string  `json:"partner_id"`
	PartnerKey      string  `json:"partner_key"`
	AppType         string  `json:"app_type"`
	APIHost         *string `json:"api_host"`
	AuthHost        *string `json:"auth_host"`
	RedirectURI     *string `json:"redirect_uri"`
	PushCallbackURL *string `json:"push_callback_url"`
	Enabled         *bool   `json:"enabled"`
}

// UpdateAppInput changes a partner app. Nil fields are unchanged; an empty
// push_callback_url clears it.
type UpdateAppInput struct {
	Name            *string `json:"name"`
	PartnerKey      *string `json:"partner_key"`
	AppType         *string `json:"app_type"`
	APIHost         *string `json:"api_host"`
	AuthHost        *string `json:"auth_host"`
	RedirectURI     *string `json:"redirect_uri"`
	PushCallbackURL *string `json:"push_callback_url"`
	Enabled         *bool   `json:"enabled"`
}

func requireAdmin(p authctx.Principal) error {
	if !p.IsPlatformAdmin {
		return apperr.NotFound("Không tìm thấy đường dẫn này.")
	}
	return nil
}

func (s *Service) checkURL(field, raw string, v *validate.Errors) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	ok := err == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || (s.AllowInsecureHosts && u.Scheme == "http"))
	if !ok {
		v.Add(field, "invalid_url", "Phải là URL đầy đủ bắt đầu bằng https://.")
	}
	return raw
}

// ListApps returns every partner app (platform admins only).
func (s *Service) ListApps(ctx context.Context, p authctx.Principal) ([]App, error) {
	if err := requireAdmin(p); err != nil {
		return nil, err
	}
	var out []App
	err := s.tx(ctx, db.Scope{UserID: p.UserID}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		rows, err := q.ListApps(ctx)
		for _, r := range rows {
			out = append(out, toApp(r))
		}
		return err
	})
	if out == nil {
		out = []App{}
	}
	return out, wrap(err, nil)
}

// CreateApp registers a partner app with its encrypted partner key.
func (s *Service) CreateApp(ctx context.Context, p authctx.Principal, in CreateAppInput) (App, error) {
	if err := requireAdmin(p); err != nil {
		return App{}, err
	}
	var v validate.Errors
	v.Length("name", strings.TrimSpace(in.Name), 1, 100)
	hosts, okEnv := defaultHosts[in.Environment]
	if !okEnv {
		v.Add("environment", "invalid", "Chỉ được là sandbox hoặc production.")
	}
	partnerID, err := strconv.ParseInt(in.PartnerID, 10, 64)
	if err != nil || partnerID <= 0 {
		v.Add("partner_id", "invalid", "Phải là số nguyên dương.")
	}
	if len(in.PartnerKey) < 16 || len(in.PartnerKey) > 256 {
		v.Add("partner_key", "invalid", "Cần từ 16 đến 256 ký tự.")
	}
	v.Length("app_type", strings.TrimSpace(in.AppType), 1, 64)
	id := db.NewID()
	apiHost := s.checkURL("api_host", deref(in.APIHost, hosts.API), &v)
	authHost := s.checkURL("auth_host", deref(in.AuthHost, hosts.Auth), &v)
	redirect := s.checkURL("redirect_uri", deref(in.RedirectURI, s.defaultRedirectURI()), &v)
	var push *string
	if raw := deref(in.PushCallbackURL, s.defaultPushURL(id)); raw != "" {
		raw = s.checkURL("push_callback_url", raw, &v)
		push = &raw
	}
	if err := v.Err(); err != nil {
		return App{}, err
	}

	row, wk, err := s.Keys.NewRow(appsTable, id.String())
	if err != nil {
		return App{}, apperr.Internal(err)
	}
	keyEnc, err := row.Seal("partner_key", []byte(in.PartnerKey))
	if err != nil {
		return App{}, apperr.Internal(err)
	}
	var out App
	err = s.tx(ctx, db.Scope{UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		a, err := q.InsertApp(ctx, shopsdb.InsertAppParams{
			ID: id, Name: strings.TrimSpace(in.Name), Environment: in.Environment, ApiHost: apiHost, AuthHost: authHost,
			PartnerID: partnerID, PartnerKeyEnc: keyEnc, DekWrapped: wk.DEK, KekID: wk.KEKID,
			AppType: strings.TrimSpace(in.AppType), RedirectUri: redirect, PushCallbackUrl: push, Enabled: deref(in.Enabled, true),
		})
		if err != nil {
			if db.UniqueViolation(err) != "" {
				return apperr.Conflict("app_exists", "Đã có ứng dụng với partner ID này trong môi trường này.")
			}
			return err
		}
		out = toApp(a)
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "shopee_app.created",
			TargetType: "shopee_app", TargetID: &id,
			Metadata: map[string]any{"environment": a.Environment, "partner_id": a.PartnerID}})
	})
	return out, wrap(err, nil)
}

// UpdateApp changes a partner app. A new partner key is re-encrypted.
func (s *Service) UpdateApp(ctx context.Context, p authctx.Principal, id uuid.UUID, in UpdateAppInput) (App, error) {
	if err := requireAdmin(p); err != nil {
		return App{}, err
	}
	var out App
	err := s.tx(ctx, db.Scope{UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		cur, err := q.GetAppForUpdate(ctx, id)
		if err != nil {
			return wrap(err, apperr.NotFound("Không tìm thấy ứng dụng Shopee."))
		}
		var v validate.Errors
		params := shopsdb.UpdateAppParams{ID: id, Name: cur.Name, ApiHost: cur.ApiHost, AuthHost: cur.AuthHost,
			PartnerKeyEnc: cur.PartnerKeyEnc, DekWrapped: cur.DekWrapped, KekID: cur.KekID, AppType: cur.AppType,
			RedirectUri: cur.RedirectUri, PushCallbackUrl: cur.PushCallbackUrl, Enabled: cur.Enabled}
		changed := []string{}
		if in.Name != nil {
			v.Length("name", strings.TrimSpace(*in.Name), 1, 100)
			params.Name = strings.TrimSpace(*in.Name)
			changed = append(changed, "name")
		}
		if in.AppType != nil {
			v.Length("app_type", strings.TrimSpace(*in.AppType), 1, 64)
			params.AppType = strings.TrimSpace(*in.AppType)
			changed = append(changed, "app_type")
		}
		if in.APIHost != nil {
			params.ApiHost = s.checkURL("api_host", *in.APIHost, &v)
			changed = append(changed, "api_host")
		}
		if in.AuthHost != nil {
			params.AuthHost = s.checkURL("auth_host", *in.AuthHost, &v)
			changed = append(changed, "auth_host")
		}
		if in.RedirectURI != nil {
			params.RedirectUri = s.checkURL("redirect_uri", *in.RedirectURI, &v)
			changed = append(changed, "redirect_uri")
		}
		if in.PushCallbackURL != nil {
			params.PushCallbackUrl = nil
			if *in.PushCallbackURL != "" {
				raw := s.checkURL("push_callback_url", *in.PushCallbackURL, &v)
				params.PushCallbackUrl = &raw
			}
			changed = append(changed, "push_callback_url")
		}
		if in.Enabled != nil {
			params.Enabled = *in.Enabled
			changed = append(changed, "enabled")
		}
		if in.PartnerKey != nil {
			if len(*in.PartnerKey) < 16 || len(*in.PartnerKey) > 256 {
				v.Add("partner_key", "invalid", "Cần từ 16 đến 256 ký tự.")
			} else {
				row, wk, err := s.Keys.NewRow(appsTable, id.String())
				if err != nil {
					return err
				}
				enc, err := row.Seal("partner_key", []byte(*in.PartnerKey))
				if err != nil {
					return err
				}
				params.PartnerKeyEnc, params.DekWrapped, params.KekID = enc, wk.DEK, wk.KEKID
			}
			changed = append(changed, "partner_key")
		}
		if err := v.Err(); err != nil {
			return err
		}
		a, err := q.UpdateApp(ctx, params)
		if err != nil {
			return err
		}
		out = toApp(a)
		return s.Audit.Record(ctx, tx, audit.Entry{ActorUserID: &p.UserID, Action: "shopee_app.updated",
			TargetType: "shopee_app", TargetID: &id, Metadata: map[string]any{"fields": changed}})
	})
	return out, wrap(err, nil)
}

func (s *Service) defaultRedirectURI() string {
	return strings.TrimRight(s.BaseURL.String(), "/") + "/api/v1/oauth/shopee/callback"
}

func (s *Service) defaultPushURL(appID uuid.UUID) string {
	return strings.TrimRight(s.BaseURL.String(), "/") + "/api/v1/webhooks/shopee/" + appID.String()
}

// clientApp decrypts the partner key of an app row for one call.
func (s *Service) clientApp(a shopsdb.ShopeeApp) (shopee.App, error) {
	row, err := s.Keys.OpenRow(appsTable, a.ID.String(), envelope.WrappedKey{KEKID: a.KekID, DEK: a.DekWrapped})
	if err != nil {
		return shopee.App{}, err
	}
	key, err := row.Open("partner_key", a.PartnerKeyEnc)
	if err != nil {
		return shopee.App{}, err
	}
	return shopee.App{ID: a.ID, Host: a.ApiHost, PartnerID: a.PartnerID, PartnerKey: key}, nil
}

// loadApp reads an app outside any tenant scope (apps are global).
func (s *Service) loadApp(ctx context.Context, id uuid.UUID) (shopsdb.ShopeeApp, error) {
	var a shopsdb.ShopeeApp
	err := s.tx(ctx, db.Scope{}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		var err error
		a, err = q.GetApp(ctx, id)
		return err
	})
	return a, err
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
