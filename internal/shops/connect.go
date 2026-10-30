package shops

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"slices"
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
	"shopee-automation/internal/platform/securetoken"
	"shopee-automation/internal/rbac"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops/shopsdb"
)

const (
	credentialsTable = "shop_credentials"
	oauthTable       = "oauth_states"
	maxSelectShops   = 50
)

// ConnectResult is returned when a connect or reconnect flow starts.
type ConnectResult struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// ConnectInput optionally selects the partner app.
type ConnectInput struct {
	ShopeeAppID *uuid.UUID `json:"shopee_app_id"`
}

// Connect starts the OAuth flow for a new shop.
func (s *Service) Connect(ctx context.Context, p authctx.Principal, t authctx.Tenant, in ConnectInput) (ConnectResult, error) {
	app, err := s.pickApp(ctx, in.ShopeeAppID)
	if err != nil {
		return ConnectResult{}, err
	}
	return s.startFlow(ctx, p, t, app, nil)
}

// Reconnect starts the OAuth flow again for an existing connection.
func (s *Service) Reconnect(ctx context.Context, p authctx.Principal, t authctx.Tenant, connID uuid.UUID) (ConnectResult, error) {
	var conn shopsdb.ShopConnection
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		var err error
		conn, err = q.GetConnection(ctx, shopsdb.GetConnectionParams{OrgID: t.OrgID, ID: connID})
		return err
	})
	if err != nil {
		return ConnectResult{}, wrap(err, errShopNotFound)
	}
	app, err := s.loadApp(ctx, conn.ShopeeAppID)
	if err != nil {
		return ConnectResult{}, wrap(err, nil)
	}
	if !app.Enabled {
		return ConnectResult{}, apperr.FailedDependency("shopee_app_disabled", "The Shopee app for this shop is disabled. Contact the platform administrator.")
	}
	return s.startFlow(ctx, p, t, app, &conn.ID)
}

func (s *Service) pickApp(ctx context.Context, id *uuid.UUID) (shopsdb.ShopeeApp, error) {
	var apps []shopsdb.ShopeeApp
	err := s.tx(ctx, db.Scope{}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		var err error
		apps, err = q.ListEnabledApps(ctx)
		return err
	})
	if err != nil {
		return shopsdb.ShopeeApp{}, apperr.Internal(err)
	}
	if id != nil {
		for _, a := range apps {
			if a.ID == *id {
				return a, nil
			}
		}
		return shopsdb.ShopeeApp{}, apperr.Invalid(apperr.FieldError{Field: "shopee_app_id", Code: "invalid", Message: "Unknown or disabled Shopee app."})
	}
	switch len(apps) {
	case 0:
		return shopsdb.ShopeeApp{}, apperr.FailedDependency("no_shopee_app", "No Shopee app is configured yet. Ask the platform administrator to add one.")
	case 1:
		return apps[0], nil
	default:
		return shopsdb.ShopeeApp{}, apperr.Invalid(apperr.FieldError{Field: "shopee_app_id", Code: "required", Message: "Choose which Shopee app to use."})
	}
}

func (s *Service) startFlow(ctx context.Context, p authctx.Principal, t authctx.Tenant, app shopsdb.ShopeeApp, connID *uuid.UUID) (ConnectResult, error) {
	token, hash := stateToken(t.OrgID)
	now := s.now()
	expires := now.Add(oauthStateTTL)
	id := db.NewID()
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		if err := q.InsertOAuthState(ctx, shopsdb.InsertOAuthStateParams{ID: id, OrgID: t.OrgID, UserID: p.UserID,
			ShopeeAppID: app.ID, TokenHash: hash, ShopConnectionID: connID, ExpiresAt: expires, CreatedAt: now}); err != nil {
			return err
		}
		action := "shop.connect_started"
		if connID != nil {
			action = "shop.reconnect_started"
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: action,
			TargetType: "shop_connection", TargetID: connID, Metadata: map[string]any{"shopee_app_id": app.ID.String()}})
	})
	if err != nil {
		return ConnectResult{}, wrap(err, nil)
	}
	return ConnectResult{AuthorizationURL: shopee.AuthURL(app.AuthHost, app.PartnerID, app.RedirectUri, token), ExpiresAt: expires}, nil
}

// CallbackInput is the query string Shopee appends to the redirect URL.
type CallbackInput struct {
	Code          string
	ShopID        string
	MainAccountID string
	State         string
}

// Callback errors are returned as stable codes in the dashboard redirect.
const (
	cbInvalidState     = "invalid_state"
	cbStateExpired     = "state_expired"
	cbStateUsed        = "state_used"
	cbDenied           = "authorization_denied"
	cbExchangeFailed   = "token_exchange_failed"
	cbConnectedElse    = "shop_connected_elsewhere"
	cbShopMismatch     = "shop_mismatch"
	cbAppDisabled      = "shopee_app_disabled"
	cbPermissionDenied = "permission_denied"
	cbInternal         = "internal_error"
)

// Callback finishes the OAuth flow and returns the dashboard URL to redirect
// to. It never returns an error: failures become ?connect_error=<code>.
func (s *Service) Callback(ctx context.Context, p authctx.Principal, in CallbackInput) string {
	orgID, ok := parseStateToken(in.State)
	if !ok {
		return s.dashboardURL("/orgs", url.Values{"connect_error": {cbInvalidState}})
	}
	fail := func(code string) string {
		return s.dashboardURL("/orgs/"+orgID.String()+"/shops", url.Values{"connect_error": {code}})
	}

	// 1. Consume the state. It is single-use even if the rest fails.
	var st shopsdb.OauthState
	code := ""
	err := s.tx(ctx, db.Scope{OrgID: orgID, UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		var err error
		st, err = q.GetOAuthStateByHash(ctx, shopsdb.GetOAuthStateByHashParams{OrgID: orgID, TokenHash: securetoken.Hash(in.State)})
		switch {
		case db.IsNoRows(err):
			code = cbInvalidState
			return nil
		case err != nil:
			return err
		case st.UserID != p.UserID: // a state is bound to the user who started the flow
			code = cbInvalidState
			return nil
		case st.ConsumedAt != nil:
			code = cbStateUsed
			return nil
		case !s.now().Before(st.ExpiresAt):
			code = cbStateExpired
			return nil
		}
		return q.ConsumeOAuthState(ctx, shopsdb.ConsumeOAuthStateParams{Now: s.now(), OrgID: orgID, ID: st.ID})
	})
	if err != nil {
		s.Log.ErrorContext(ctx, "oauth callback: consume state", slog.String("error", err.Error()))
		return fail(cbInternal)
	}
	if code == cbInvalidState && st.ID == uuid.Nil {
		return s.dashboardURL("/orgs", url.Values{"connect_error": {cbInvalidState}})
	}
	if code != "" {
		return fail(code)
	}

	// 2. The user must still be allowed to connect shops in this org.
	t, err := s.Tenants.ResolveTenant(ctx, p.UserID, orgID)
	if err != nil || !t.Can(rbac.ShopsConnect) {
		return s.dashboardURL("/orgs", url.Values{"connect_error": {cbPermissionDenied}})
	}

	shopID, _ := strconv.ParseInt(in.ShopID, 10, 64)
	mainID, _ := strconv.ParseInt(in.MainAccountID, 10, 64)
	if in.Code == "" || (shopID <= 0) == (mainID <= 0) {
		s.auditFailure(ctx, p, orgID, "shop.connect_failed", cbDenied, nil)
		return fail(cbDenied)
	}
	if st.ShopConnectionID != nil && mainID > 0 {
		return fail(cbShopMismatch) // reconnect expects the same single shop
	}

	appRow, err := s.loadApp(ctx, st.ShopeeAppID)
	if err != nil || !appRow.Enabled {
		return fail(cbAppDisabled)
	}
	app, err := s.clientApp(appRow)
	if err != nil {
		s.Log.ErrorContext(ctx, "oauth callback: decrypt partner key", slog.String("error", err.Error()))
		return fail(cbInternal)
	}

	tokens, err := s.Shopee.GetAccessToken(ctx, app, in.Code, shopID, mainID)
	if err != nil {
		s.Log.WarnContext(ctx, "oauth callback: token exchange failed", slog.String("error", err.Error()))
		s.auditFailure(ctx, p, orgID, "shop.connect_failed", cbExchangeFailed, shopeeMeta(err))
		return fail(cbExchangeFailed)
	}

	// 3a. Main account: let the user choose which shops to attach.
	if mainID > 0 {
		if err := s.storePending(ctx, p, orgID, st.ID, mainID, tokens); err != nil {
			s.Log.ErrorContext(ctx, "oauth callback: store pending selection", slog.String("error", err.Error()))
			return fail(cbInternal)
		}
		return s.dashboardURL("/orgs/"+orgID.String()+"/shops/select", url.Values{"state": {st.ID.String()}})
	}

	// 3b. Single shop.
	var conn shopsdb.ShopConnection
	err = s.tx(ctx, db.Scope{OrgID: orgID, UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		if st.ShopConnectionID != nil {
			target, err := q.LockConnection(ctx, shopsdb.LockConnectionParams{OrgID: orgID, ID: *st.ShopConnectionID})
			if err != nil {
				return err
			}
			if target.ShopeeShopID != shopID {
				return errMismatch
			}
		}
		var err error
		conn, err = s.attachShop(ctx, q, tx, p.UserID, orgID, appRow.ID, shopID, tokens, nil)
		return err
	})
	switch {
	case errors.Is(err, errMismatch):
		s.auditFailure(ctx, p, orgID, "shop.reconnect_failed", cbShopMismatch, map[string]any{"shopee_shop_id": shopID})
		return fail(cbShopMismatch)
	case errors.Is(err, errElsewhere):
		s.auditFailure(ctx, p, orgID, "shop.connect_failed", cbConnectedElse, map[string]any{"shopee_shop_id": shopID})
		return fail(cbConnectedElse)
	case err != nil:
		s.Log.ErrorContext(ctx, "oauth callback: attach shop", slog.String("error", err.Error()))
		return fail(cbInternal)
	}
	s.enqueueSync(ctx, orgID, conn.ID)
	return s.dashboardURL("/orgs/"+orgID.String()+"/shops/"+conn.ID.String(), url.Values{"connected": {"1"}})
}

var (
	errMismatch  = errors.New("shop mismatch")
	errElsewhere = errors.New("shop connected elsewhere")
)

// attachShop creates or re-activates a connection and stores its tokens.
// It must run inside a tenant-scoped transaction.
func (s *Service) attachShop(ctx context.Context, q *shopsdb.Queries, tx pgx.Tx, userID, orgID, appID uuid.UUID,
	shopID int64, tokens shopee.Tokens, detail map[string]any) (shopsdb.ShopConnection, error) {
	now := s.now()
	conn, err := q.GetConnectionByShop(ctx, shopsdb.GetConnectionByShopParams{OrgID: orgID, ShopeeAppID: appID, ShopeeShopID: shopID})
	event, action := "REAUTHORIZED", "shop.reconnected"
	switch {
	case db.IsNoRows(err):
		event, action = "GRANTED", "shop.connected"
		conn, err = q.InsertConnection(ctx, shopsdb.InsertConnectionParams{ID: db.NewID(), OrgID: orgID, ShopeeAppID: appID,
			ShopeeShopID: shopID, Status: StatusActive, ConnectedBy: &userID, ConnectedAt: now})
	case err == nil:
		conn, err = q.ReactivateConnection(ctx, shopsdb.ReactivateConnectionParams{ConnectedBy: &userID, ConnectedAt: now, OrgID: orgID, ID: conn.ID})
	}
	if err != nil {
		if db.UniqueViolation(err) == "shop_connections_live_shop_uk" {
			return conn, errElsewhere
		}
		return conn, err
	}
	if err := s.storeTokens(ctx, q, orgID, conn.ID, tokens); err != nil {
		return conn, err
	}
	if detail == nil {
		detail = map[string]any{}
	}
	detail["request_id"] = tokens.RequestID
	if err := s.recordAuthorization(ctx, q, orgID, conn.ID, event, "callback", detail); err != nil {
		return conn, err
	}
	return conn, s.Audit.Record(ctx, tx, audit.Entry{OrgID: &orgID, ActorUserID: &userID, Action: action,
		TargetType: "shop_connection", TargetID: &conn.ID, Metadata: map[string]any{"shopee_shop_id": shopID}})
}

// storeTokens encrypts and saves a token pair under a fresh data key.
func (s *Service) storeTokens(ctx context.Context, q *shopsdb.Queries, orgID, connID uuid.UUID, t shopee.Tokens) error {
	now := s.now()
	row, wk, err := s.Keys.NewRow(credentialsTable, connID.String())
	if err != nil {
		return err
	}
	at, err := row.Seal("access_token", []byte(t.AccessToken))
	if err != nil {
		return err
	}
	rt, err := row.Seal("refresh_token", []byte(t.RefreshToken))
	if err != nil {
		return err
	}
	return q.UpsertCredentials(ctx, shopsdb.UpsertCredentialsParams{ShopConnectionID: connID, OrgID: orgID,
		AccessTokenEnc: at, RefreshTokenEnc: rt, DekWrapped: wk.DEK, KekID: wk.KEKID,
		AccessExpiresAt: now.Add(t.AccessTTL(now)), RefreshExpiresAt: now.Add(shopee.RefreshTokenLifetime), Now: now})
}

func (s *Service) recordAuthorization(ctx context.Context, q *shopsdb.Queries, orgID, connID uuid.UUID, event, source string, detail map[string]any) error {
	return s.recordAuthorizationAt(ctx, q, orgID, connID, event, source, nil, nil, detail)
}

func (s *Service) recordAuthorizationAt(ctx context.Context, q *shopsdb.Queries, orgID, connID uuid.UUID, event, source string,
	authTime, expireTime *time.Time, detail map[string]any) error {
	b, _ := json.Marshal(nonNilMap(detail))
	return q.InsertAuthorization(ctx, shopsdb.InsertAuthorizationParams{ID: db.NewID(), OrgID: orgID, ShopConnectionID: connID,
		Event: event, Source: source, AuthTime: authTime, ExpireTime: expireTime, Detail: b, CreatedAt: s.now()})
}

// pendingTokens is what waits (encrypted) for a main-account selection.
type pendingTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpireIn     int64  `json:"expire_in"`
	RequestID    string `json:"request_id"`
}

func (s *Service) storePending(ctx context.Context, p authctx.Principal, orgID, stateID uuid.UUID, mainID int64, t shopee.Tokens) error {
	row, wk, err := s.Keys.NewRow(oauthTable, stateID.String())
	if err != nil {
		return err
	}
	// #nosec G117 -- serialized only to be encrypted on the next line
	b, _ := json.Marshal(pendingTokens{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, ExpireIn: t.ExpireIn, RequestID: t.RequestID})
	enc, err := row.Seal("pending_tokens", b)
	if err != nil {
		return err
	}
	ids := t.ShopIDList
	if ids == nil {
		ids = []int64{}
	}
	return s.tx(ctx, db.Scope{OrgID: orgID, UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		if err := q.SetOAuthPending(ctx, shopsdb.SetOAuthPendingParams{MainAccountID: &mainID, PendingShopIds: ids,
			PendingTokensEnc: enc, PendingDekWrapped: wk.DEK, PendingKekID: &wk.KEKID,
			PendingExpiresAt: ptr(s.now().Add(selectionTTL)), OrgID: orgID, ID: stateID}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &orgID, ActorUserID: &p.UserID, Action: "shop.main_account_authorized",
			Metadata: map[string]any{"main_account_id": mainID, "shop_count": len(ids)}})
	})
}

// PendingShop is one shop offered for selection after a main-account login.
type PendingShop struct {
	ShopeeShopID string  `json:"shopee_shop_id"`
	ShopName     *string `json:"shop_name"`
	Region       *string `json:"region"`
}

// Selection describes a pending main-account authorization.
type Selection struct {
	ID        uuid.UUID     `json:"id"`
	Shops     []PendingShop `json:"shops"`
	ExpiresAt time.Time     `json:"expires_at"`
}

func (s *Service) loadSelection(ctx context.Context, q *shopsdb.Queries, p authctx.Principal, t authctx.Tenant, stateID uuid.UUID) (shopsdb.OauthState, pendingTokens, error) {
	notFound := apperr.NotFound("Shop selection not found or expired.")
	st, err := q.GetOAuthState(ctx, shopsdb.GetOAuthStateParams{OrgID: t.OrgID, ID: stateID})
	if err != nil {
		return st, pendingTokens{}, wrap(err, notFound)
	}
	if st.UserID != p.UserID || st.PendingTokensEnc == nil || st.SelectedAt != nil ||
		st.PendingExpiresAt == nil || !s.now().Before(*st.PendingExpiresAt) || st.PendingKekID == nil {
		return st, pendingTokens{}, notFound
	}
	row, err := s.Keys.OpenRow(oauthTable, st.ID.String(), envelope.WrappedKey{KEKID: *st.PendingKekID, DEK: st.PendingDekWrapped})
	if err != nil {
		return st, pendingTokens{}, apperr.Internal(err)
	}
	b, err := row.Open("pending_tokens", st.PendingTokensEnc)
	if err != nil {
		return st, pendingTokens{}, apperr.Internal(err)
	}
	var pt pendingTokens
	if err := json.Unmarshal(b, &pt); err != nil {
		return st, pendingTokens{}, apperr.Internal(err)
	}
	return st, pt, nil
}

// GetSelection lists the shops of a pending main-account authorization. Shop
// names are read from Shopee with the shared token (best effort).
func (s *Service) GetSelection(ctx context.Context, p authctx.Principal, t authctx.Tenant, stateID uuid.UUID) (Selection, error) {
	var st shopsdb.OauthState
	var pt pendingTokens
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		var err error
		st, pt, err = s.loadSelection(ctx, q, p, t, stateID)
		return err
	})
	if err != nil {
		return Selection{}, err
	}
	out := Selection{ID: st.ID, ExpiresAt: st.PendingExpiresAt.UTC(), Shops: []PendingShop{}}
	appRow, aerr := s.loadApp(ctx, st.ShopeeAppID)
	app, cerr := s.clientApp(appRow)
	for i, id := range st.PendingShopIds {
		ps := PendingShop{ShopeeShopID: strconv.FormatInt(id, 10)}
		if aerr == nil && cerr == nil && i < maxSelectShops {
			if info, err := s.Shopee.GetShopInfo(ctx, app, pt.AccessToken, id); err == nil {
				ps.ShopName, ps.Region = &info.ShopName, &info.Region
			}
		}
		out.Shops = append(out.Shops, ps)
	}
	return out, nil
}

// SelectInput lists the Shopee shop IDs to attach.
type SelectInput struct {
	ShopeeShopIDs []string `json:"shopee_shop_ids"`
}

// SelectShops attaches the chosen shops of a main-account authorization.
func (s *Service) SelectShops(ctx context.Context, p authctx.Principal, t authctx.Tenant, stateID uuid.UUID, in SelectInput) ([]Shop, error) {
	if len(in.ShopeeShopIDs) == 0 || len(in.ShopeeShopIDs) > maxSelectShops {
		return nil, apperr.Invalid(apperr.FieldError{Field: "shopee_shop_ids", Code: "invalid", Message: "Choose between 1 and 50 shops."})
	}
	var chosen []int64
	for _, raw := range in.ShopeeShopIDs {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 || slices.Contains(chosen, id) {
			return nil, apperr.Invalid(apperr.FieldError{Field: "shopee_shop_ids", Code: "invalid", Message: "Shop IDs must be unique positive integers."})
		}
		chosen = append(chosen, id)
	}
	var conns []shopsdb.ShopConnection
	env := ""
	err := s.tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		st, pt, err := s.loadSelection(ctx, q, p, t, stateID)
		if err != nil {
			return err
		}
		for _, id := range chosen {
			if !slices.Contains(st.PendingShopIds, id) {
				return apperr.Invalid(apperr.FieldError{Field: "shopee_shop_ids", Code: "not_authorized", Message: "Shop " + strconv.FormatInt(id, 10) + " was not part of this authorization."})
			}
		}
		app, err := q.GetApp(ctx, st.ShopeeAppID)
		if err != nil {
			return err
		}
		env = app.Environment
		tokens := shopee.Tokens{AccessToken: pt.AccessToken, RefreshToken: pt.RefreshToken, ExpireIn: pt.ExpireIn, RequestID: pt.RequestID}
		for _, id := range chosen {
			c, err := s.attachShop(ctx, q, tx, p.UserID, t.OrgID, st.ShopeeAppID, id, tokens, map[string]any{"main_account_id": deref(st.MainAccountID, 0)})
			if errors.Is(err, errElsewhere) {
				return apperr.Conflict(cbConnectedElse, "Shop "+strconv.FormatInt(id, 10)+" is already connected to another organization.")
			}
			if err != nil {
				return err
			}
			conns = append(conns, c)
		}
		return q.FinishOAuthSelection(ctx, shopsdb.FinishOAuthSelectionParams{Now: s.now(), OrgID: t.OrgID, ID: st.ID})
	})
	if err != nil {
		return nil, wrap(err, nil)
	}
	out := make([]Shop, 0, len(conns))
	for _, c := range conns {
		s.enqueueSync(ctx, t.OrgID, c.ID)
		out = append(out, toShop(c, env))
	}
	return out, nil
}

func (s *Service) auditFailure(ctx context.Context, p authctx.Principal, orgID uuid.UUID, action, code string, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["reason"] = code
	err := s.tx(ctx, db.Scope{OrgID: orgID, UserID: p.UserID}, func(_ *shopsdb.Queries, tx pgx.Tx) error {
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &orgID, ActorUserID: &p.UserID, Action: action, Outcome: audit.Failure, Metadata: meta})
	})
	if err != nil {
		s.Log.WarnContext(ctx, "audit failure event", slog.String("error", err.Error()))
	}
}

func shopeeMeta(err error) map[string]any {
	if e, ok := shopee.AsError(err); ok {
		return map[string]any{"shopee_error": e.Code, "shopee_request_id": e.RequestID}
	}
	return map[string]any{}
}

func (s *Service) dashboardURL(path string, q url.Values) string {
	u := strings.TrimRight(s.BaseURL.String(), "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// State tokens embed the org ID so the callback can set the tenant scope
// before the lookup: "<org uuid hex>.<random>".
func stateToken(orgID uuid.UUID) (string, []byte) {
	random, _ := securetoken.New()
	token := strings.ReplaceAll(orgID.String(), "-", "") + "." + random
	return token, securetoken.Hash(token)
}

func parseStateToken(token string) (uuid.UUID, bool) {
	orgPart, rest, ok := strings.Cut(token, ".")
	if !ok || len(orgPart) != 32 || len(rest) < 32 || len(token) > 128 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(orgPart)
	return id, err == nil
}

func ptr[T any](v T) *T { return &v }

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
