package shops

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/capability"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops/shopsdb"
)

// ListFilter narrows the shop list.
type ListFilter struct {
	Status *string
	Region *string
	Q      *string
}

var validStatuses = []string{StatusPending, StatusActive, StatusReconnectRequired, StatusRevoked, StatusExpired, StatusDisconnected}

// List returns the org's shops.
func (s *Service) List(ctx context.Context, t authctx.Tenant, f ListFilter) ([]Shop, error) {
	if f.Status != nil {
		ok := false
		for _, st := range validStatuses {
			ok = ok || st == *f.Status
		}
		if !ok {
			return nil, apperr.Invalid(apperr.FieldError{Field: "status", Code: "invalid", Message: "Trạng thái không hợp lệ."})
		}
	}
	if f.Q != nil {
		q := strings.TrimSpace(*f.Q)
		if len(q) > 100 {
			return nil, apperr.Invalid(apperr.FieldError{Field: "q", Code: "too_long", Message: "Tối đa 100 ký tự."})
		}
		// Escape LIKE wildcards so the search is literal.
		q = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
		f.Q = &q
	}
	var out []Shop
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		envs, err := s.appEnvironments(ctx, q)
		if err != nil {
			return err
		}
		rows, err := q.ListConnections(ctx, shopsdb.ListConnectionsParams{OrgID: t.OrgID, Status: f.Status, Region: f.Region, Q: f.Q})
		for _, r := range rows {
			out = append(out, toShop(r, envs[r.ShopeeAppID]))
		}
		return err
	})
	if out == nil {
		out = []Shop{}
	}
	return out, wrap(err, nil)
}

func (s *Service) appEnvironments(ctx context.Context, q *shopsdb.Queries) (map[uuid.UUID]string, error) {
	apps, err := q.ListApps(ctx)
	m := map[uuid.UUID]string{}
	for _, a := range apps {
		m[a.ID] = a.Environment
	}
	return m, err
}

// Profile is the last synced Shopee profile.
type Profile struct {
	ShopName    *string   `json:"shop_name"`
	Region      *string   `json:"region"`
	Status      *string   `json:"status"`
	LogoURL     *string   `json:"shop_logo_url"`
	Description *string   `json:"description"`
	IsCB        *bool     `json:"is_cb"`
	IsSIP       *bool     `json:"is_sip"`
	MerchantID  *string   `json:"merchant_id"`
	FetchedAt   time.Time `json:"fetched_at"`
}

// AuthorizationEvent is one entry of the connection history.
type AuthorizationEvent struct {
	Event      string     `json:"event"`
	Source     string     `json:"source"`
	ExpireTime *time.Time `json:"expire_time"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Detail is a shop with its profile, capabilities and history.
type Detail struct {
	Shop         Shop                  `json:"shop"`
	Profile      *Profile              `json:"profile"`
	Capabilities []capability.Resolved `json:"capabilities"`
	History      []AuthorizationEvent  `json:"history"`
}

// Get returns one shop.
func (s *Service) Get(ctx context.Context, t authctx.Tenant, id uuid.UUID) (Detail, error) {
	var d Detail
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		conn, err := q.GetConnection(ctx, shopsdb.GetConnectionParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errShopNotFound)
		}
		app, err := q.GetApp(ctx, conn.ShopeeAppID)
		if err != nil {
			return err
		}
		d.Shop = toShop(conn, app.Environment)
		if p, err := q.GetProfile(ctx, shopsdb.GetProfileParams{OrgID: t.OrgID, ShopConnectionID: id}); err == nil {
			d.Profile = toProfile(p)
		} else if !db.IsNoRows(err) {
			return err
		}
		if d.Capabilities, err = s.resolve(ctx, q, tx, conn, app); err != nil {
			return err
		}
		hist, err := q.ListAuthorizations(ctx, shopsdb.ListAuthorizationsParams{OrgID: t.OrgID, ShopConnectionID: id, Lim: 20})
		d.History = []AuthorizationEvent{}
		for _, h := range hist {
			d.History = append(d.History, AuthorizationEvent{Event: h.Event, Source: h.Source, ExpireTime: utcPtr(h.ExpireTime), CreatedAt: h.CreatedAt.UTC()})
		}
		return err
	})
	return d, wrap(err, nil)
}

func toProfile(p shopsdb.ShopProfile) *Profile {
	var merchant *string
	if p.MerchantID != nil {
		m := strconv.FormatInt(*p.MerchantID, 10)
		merchant = &m
	}
	return &Profile{ShopName: p.ShopName, Region: p.Region, Status: p.Status, LogoURL: p.ShopLogoUrl,
		Description: p.Description, IsCB: p.IsCb, IsSIP: p.IsSip, MerchantID: merchant, FetchedAt: p.FetchedAt.UTC()}
}

// Capabilities returns the resolved capabilities of one shop.
func (s *Service) Capabilities(ctx context.Context, t authctx.Tenant, id uuid.UUID) ([]capability.Resolved, error) {
	var out []capability.Resolved
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		conn, err := q.GetConnection(ctx, shopsdb.GetConnectionParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errShopNotFound)
		}
		app, err := q.GetApp(ctx, conn.ShopeeAppID)
		if err != nil {
			return err
		}
		out, err = s.resolve(ctx, q, tx, conn, app)
		return err
	})
	return out, wrap(err, nil)
}

func (s *Service) resolve(ctx context.Context, q *shopsdb.Queries, tx pgx.Tx, conn shopsdb.ShopConnection, app shopsdb.ShopeeApp) ([]capability.Resolved, error) {
	defs, err := q.ListCapabilityDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	ovs, err := q.ListCapabilityOverrides(ctx, &app.ID)
	if err != nil {
		return nil, err
	}
	cd := make([]capability.Definition, 0, len(defs))
	for _, d := range defs {
		cd = append(cd, capability.Definition{Key: d.Key, Title: d.Title, DefaultMode: capability.Mode(d.DefaultMode),
			FallbackMode: capability.Mode(deref(d.FallbackMode, "")), RequiredAppTypes: d.RequiredAppTypes,
			SupportedRegions: d.SupportedRegions, RequiresApproval: d.RequiresApproval, Sensitive: d.Sensitive,
			Enabled: d.Enabled, Verification: d.Verification})
	}
	co := make([]capability.Override, 0, len(ovs))
	for _, o := range ovs {
		var mode *capability.Mode
		if o.Mode != nil {
			m := capability.Mode(*o.Mode)
			mode = &m
		}
		co = append(co, capability.Override{CapabilityKey: o.CapabilityKey, AppID: o.ShopeeAppID, Region: o.Region,
			Mode: mode, Enabled: o.Enabled, Reason: o.Reason})
	}
	cctx := capability.Context{AppID: app.ID, AppType: app.AppType, AppEnvironment: app.Environment,
		Region: deref(conn.Region, ""), ConnectionStatus: conn.Status}
	if s.Extension != nil {
		if cctx.ExtensionAvailable, cctx.BrowserReady, err = s.Extension.Ready(ctx, tx, conn.OrgID, cctx.Region); err != nil {
			return nil, err
		}
	}
	return capability.Resolve(cd, co, cctx), nil
}

// SyncJob is returned when a sync is queued.
type SyncJob struct {
	JobID string `json:"job_id"`
}

// RequestSync queues a sync of one shop (deduplicated per minute).
func (s *Service) RequestSync(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) (SyncJob, error) {
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		conn, err := q.GetConnection(ctx, shopsdb.GetConnectionParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errShopNotFound)
		}
		if conn.Status != StatusActive {
			return errReconnectRequired
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "shop.sync_requested",
			TargetType: "shop_connection", TargetID: &id})
	})
	if err != nil {
		return SyncJob{}, wrap(err, nil)
	}
	jobID := "sync:" + id.String() + ":" + strconv.FormatInt(s.now().Unix()/60, 10)
	if _, err := s.Queue.Enqueue(ctx, queue.TypeShopSync, SyncPayload{OrgID: t.OrgID, ConnectionID: id},
		queue.Options{ID: jobID, MaxRetry: 3, Timeout: 2 * time.Minute}); err != nil {
		return SyncJob{}, apperr.Unavailable("queue_unavailable", "Chưa thể bắt đầu đồng bộ. Hãy thử lại sau ít phút.")
	}
	return SyncJob{JobID: jobID}, nil
}

func (s *Service) enqueueSync(ctx context.Context, orgID, connID uuid.UUID) {
	id := "sync:" + connID.String() + ":connect:" + strconv.FormatInt(s.now().UnixNano(), 10)
	if _, err := s.Queue.Enqueue(ctx, queue.TypeShopSync, SyncPayload{OrgID: orgID, ConnectionID: connID},
		queue.Options{ID: id, MaxRetry: 3, Timeout: 2 * time.Minute}); err != nil {
		s.Log.WarnContext(ctx, "enqueue sync after connect", slog.String("error", err.Error()))
	}
}

// SyncPayload is the task payload for a shop sync.
type SyncPayload struct {
	OrgID        uuid.UUID `json:"org_id"`
	ConnectionID uuid.UUID `json:"connection_id"`
}

// HandleSync is the worker handler for queue.TypeShopSync.
func (s *Service) HandleSync(ctx context.Context, payload []byte) error {
	var p SyncPayload
	if err := queue.Decode(payload, &p); err != nil {
		return err
	}
	err := s.Sync(ctx, p.OrgID, p.ConnectionID)
	if apperr.IsKind(err, apperr.KindFailedDependency) || apperr.IsKind(err, apperr.KindNotFound) {
		return nil // reconnect needed or shop gone: retrying will not help
	}
	return err
}

// Sync reads shop info and profile from Shopee and stores them.
func (s *Service) Sync(ctx context.Context, orgID, connID uuid.UUID) error {
	acc, err := s.AccessFor(ctx, orgID, connID)
	if err != nil {
		return err
	}
	info, err := s.Shopee.GetShopInfo(ctx, acc.App, acc.AccessToken, acc.ShopID)
	if shopee.IsKind(err, shopee.KindAuth) {
		// The token may have been invalidated early; refresh once and retry.
		if rerr := s.Refresh(ctx, orgID, connID, 4*time.Hour); rerr != nil {
			return rerr
		}
		if acc, err = s.AccessFor(ctx, orgID, connID); err != nil {
			return err
		}
		info, err = s.Shopee.GetShopInfo(ctx, acc.App, acc.AccessToken, acc.ShopID)
	}
	var profile shopee.Profile
	if err == nil {
		profile, err = s.Shopee.GetProfile(ctx, acc.App, acc.AccessToken, acc.ShopID)
	}
	if err != nil {
		return s.syncFailed(ctx, orgID, connID, err)
	}

	now := s.now()
	var authExpiry, authTime *time.Time
	if info.ExpireTime > 0 {
		authExpiry = ptr(time.Unix(info.ExpireTime, 0).UTC())
	}
	if info.AuthTime > 0 {
		authTime = ptr(time.Unix(info.AuthTime, 0).UTC())
	}
	raw, _ := json.Marshal(map[string]json.RawMessage{"shop_info": info.Raw, "profile": nonEmptyJSON(profile.Raw)})
	return wrap(s.tx(ctx, db.Scope{OrgID: orgID}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		conn, err := q.LockConnection(ctx, shopsdb.LockConnectionParams{OrgID: orgID, ID: connID})
		if err != nil {
			return wrap(err, errShopNotFound)
		}
		if conn.Status != StatusActive {
			return nil // disconnected while we were reading
		}
		name := info.ShopName
		if profile.ShopName != "" {
			name = profile.ShopName
		}
		if _, err := q.UpdateConnectionSynced(ctx, shopsdb.UpdateConnectionSyncedParams{Region: &info.Region, ShopName: &name,
			AuthorizationExpiresAt: authExpiry, SyncedAt: &now, OrgID: orgID, ID: connID}); err != nil {
			return err
		}
		if conn.AuthorizationExpiresAt == nil || authExpiry != nil && !conn.AuthorizationExpiresAt.Equal(*authExpiry) {
			if err := s.recordAuthorizationAt(ctx, q, orgID, connID, "GRANTED", "sync", authTime, authExpiry,
				map[string]any{"request_id": info.RequestID}); err != nil {
				return err
			}
		}
		return q.UpsertProfile(ctx, shopsdb.UpsertProfileParams{ShopConnectionID: connID, OrgID: orgID,
			ShopName: &name, Region: &info.Region, Status: &info.Status, ShopLogoUrl: emptyNil(profile.ShopLogo),
			Description: emptyNil(profile.Description), IsCb: &info.IsCB, IsSip: &info.IsSIP, MerchantID: info.MerchantID,
			Raw: raw, FetchedAt: now})
	}), nil)
}

func (s *Service) syncFailed(ctx context.Context, orgID, connID uuid.UUID, callErr error) error {
	code := "unknown"
	if e, ok := shopee.AsError(callErr); ok {
		code = e.Code
	} else if ae, ok := apperr.As(callErr); ok {
		return ae
	}
	err := s.tx(ctx, db.Scope{OrgID: orgID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		conn, err := q.LockConnection(ctx, shopsdb.LockConnectionParams{OrgID: orgID, ID: connID})
		if err != nil {
			return err
		}
		if shopee.IsKind(callErr, shopee.KindAuthorizationLost) && conn.Status == StatusActive {
			return s.markReconnectRequired(ctx, q, tx, conn, "sync", code, shopeeMeta(callErr))
		}
		return q.UpdateConnectionSyncFailed(ctx, shopsdb.UpdateConnectionSyncFailedParams{Error: &code, OrgID: orgID, ID: connID})
	})
	if err != nil {
		s.Log.ErrorContext(ctx, "record sync failure", slog.String("error", err.Error()))
	}
	return upstreamError(callErr, false)
}

// DisconnectInput confirms a disconnect.
type DisconnectInput struct {
	Confirm bool `json:"confirm"`
}

// Disconnect deletes the shop's tokens and synced Shopee data and marks the
// connection DISCONNECTED. History and audit rows are kept. The seller can
// also revoke the authorization in Shopee Seller Centre.
func (s *Service) Disconnect(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID, in DisconnectInput) error {
	if !in.Confirm {
		return apperr.Invalid(apperr.FieldError{Field: "confirm", Code: "required", Message: "Hãy xác nhận ngắt kết nối."})
	}
	return wrap(s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		conn, err := q.LockConnection(ctx, shopsdb.LockConnectionParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errShopNotFound)
		}
		if conn.Status == StatusDisconnected {
			return nil
		}
		if err := s.removeShopData(ctx, q, t.OrgID, id); err != nil {
			return err
		}
		if _, err := q.SetConnectionStatus(ctx, shopsdb.SetConnectionStatusParams{Status: StatusDisconnected, Now: s.now(), OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		if err := s.recordAuthorization(ctx, q, t.OrgID, id, "DISCONNECTED", "user", nil); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "shop.disconnected",
			TargetType: "shop_connection", TargetID: &id, Metadata: map[string]any{"shopee_shop_id": conn.ShopeeShopID}})
	}), nil)
}

// removeShopData deletes credentials and Shopee content (profile). Shopee's
// terms require deleting Shopee data after termination.
func (s *Service) removeShopData(ctx context.Context, q *shopsdb.Queries, orgID, id uuid.UUID) error {
	if err := q.DeleteCredentials(ctx, shopsdb.DeleteCredentialsParams{OrgID: orgID, ShopConnectionID: id}); err != nil {
		return err
	}
	return q.DeleteProfile(ctx, shopsdb.DeleteProfileParams{OrgID: orgID, ShopConnectionID: id})
}

func emptyNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonEmptyJSON(b json.RawMessage) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}
