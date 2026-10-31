package shops

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/rbac"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops/shopsdb"
)

// Access is what a caller needs to make one Shopee shop API call.
type Access struct {
	App         shopee.App
	AccessToken string
	ShopID      int64
	Region      string
}

// AccessFor returns a usable access token for a connection, refreshing it
// first when it expires within five minutes. The token lives only in memory.
func (s *Service) AccessFor(ctx context.Context, orgID, connID uuid.UUID) (Access, error) {
	acc, expires, err := s.readAccess(ctx, orgID, connID)
	if err != nil {
		return Access{}, err
	}
	if expires.Sub(s.now()) > refreshEarly {
		return acc, nil
	}
	if err := s.Refresh(ctx, orgID, connID, refreshEarly); err != nil {
		return Access{}, err
	}
	acc, _, err = s.readAccess(ctx, orgID, connID)
	return acc, err
}

func (s *Service) readAccess(ctx context.Context, orgID, connID uuid.UUID) (Access, time.Time, error) {
	var conn shopsdb.ShopConnection
	var cred shopsdb.ShopCredential
	var app shopsdb.ShopeeApp
	err := s.tx(ctx, db.Scope{OrgID: orgID}, func(q *shopsdb.Queries, _ pgx.Tx) error {
		var err error
		if conn, err = q.GetConnection(ctx, shopsdb.GetConnectionParams{OrgID: orgID, ID: connID}); err != nil {
			return wrap(err, errShopNotFound)
		}
		if conn.Status != StatusActive {
			return errReconnectRequired
		}
		if cred, err = q.GetCredentials(ctx, shopsdb.GetCredentialsParams{OrgID: orgID, ShopConnectionID: connID}); err != nil {
			return wrap(err, errReconnectRequired)
		}
		app, err = q.GetApp(ctx, conn.ShopeeAppID)
		return err
	})
	if err != nil {
		return Access{}, time.Time{}, wrap(err, nil)
	}
	if !app.Enabled {
		return Access{}, time.Time{}, apperr.FailedDependency("shopee_app_disabled", "Ứng dụng Shopee của shop này đang bị tắt.")
	}
	capp, err := s.clientApp(app)
	if err != nil {
		return Access{}, time.Time{}, apperr.Internal(err)
	}
	at, _, err := s.openTokens(cred)
	if err != nil {
		return Access{}, time.Time{}, apperr.Internal(err)
	}
	return Access{App: capp, AccessToken: at, ShopID: conn.ShopeeShopID, Region: deref(conn.Region, "")}, cred.AccessExpiresAt, nil
}

func (s *Service) openTokens(c shopsdb.ShopCredential) (access, refresh string, err error) {
	row, err := s.Keys.OpenRow(credentialsTable, c.ShopConnectionID.String(), envelope.WrappedKey{KEKID: c.KekID, DEK: c.DekWrapped})
	if err != nil {
		return "", "", err
	}
	a, err := row.Open("access_token", c.AccessTokenEnc)
	if err != nil {
		return "", "", err
	}
	r, err := row.Open("refresh_token", c.RefreshTokenEnc)
	if err != nil {
		return "", "", err
	}
	return string(a), string(r), nil
}

// Refresh renews the token pair unless it is still valid for longer than
// minRemaining. The credential row is locked for the whole exchange, so
// concurrent callers (API, workers, scheduler) serialize and only the first
// one calls Shopee; the others see the fresh token and return. Holding the
// lock across a bounded HTTP call is acceptable at one row per shop, and it
// stays correct when Redis is down.
func (s *Service) Refresh(ctx context.Context, orgID, connID uuid.UUID, minRemaining time.Duration) error {
	var result error
	err := s.tx(ctx, db.Scope{OrgID: orgID}, func(q *shopsdb.Queries, tx pgx.Tx) error {
		conn, err := q.LockConnection(ctx, shopsdb.LockConnectionParams{OrgID: orgID, ID: connID})
		if err != nil {
			return wrap(err, errShopNotFound)
		}
		if conn.Status != StatusActive {
			result = errReconnectRequired
			return nil
		}
		cred, err := q.GetCredentialsForUpdate(ctx, shopsdb.GetCredentialsForUpdateParams{OrgID: orgID, ShopConnectionID: connID})
		if err != nil {
			return wrap(err, errReconnectRequired)
		}
		if cred.AccessExpiresAt.Sub(s.now()) > minRemaining {
			return nil // someone else refreshed while we waited for the lock
		}
		appRow, err := q.GetApp(ctx, conn.ShopeeAppID)
		if err != nil {
			return err
		}
		app, err := s.clientApp(appRow)
		if err != nil {
			return err
		}
		_, refresh, err := s.openTokens(cred)
		if err != nil {
			return err
		}

		cctx, cancel := context.WithTimeout(ctx, refreshTimeout)
		tokens, callErr := s.Shopee.RefreshAccessToken(cctx, app, refresh, conn.ShopeeShopID)
		cancel()
		if callErr == nil {
			// Persist immediately: the old refresh token is now dead.
			if err := s.storeTokens(ctx, q, orgID, connID, tokens); err != nil {
				return err
			}
			return s.recordAuthorization(ctx, q, orgID, connID, "REFRESHED", "refresh", map[string]any{"request_id": tokens.RequestID})
		}

		result = upstreamError(callErr, false)
		code := "unknown"
		if e, ok := shopee.AsError(callErr); ok {
			code = e.Code
		}
		if shopee.IsKind(callErr, shopee.KindAuthorizationLost) {
			return s.markReconnectRequired(ctx, q, tx, conn, "refresh", code, shopeeMeta(callErr))
		}
		// Temporary or unclear failure: keep the tokens. After an ambiguous
		// failure the old refresh token may already be spent; the next
		// attempt then fails definitively and the shop is marked for
		// reconnection (see docs/shopee-integration.md §4.3, open item S-3).
		if err := q.RecordCredentialFailure(ctx, shopsdb.RecordCredentialFailureParams{Code: &code, OrgID: orgID, ShopConnectionID: connID}); err != nil {
			return err
		}
		return s.recordAuthorization(ctx, q, orgID, connID, "REFRESH_FAILED", "refresh", shopeeMeta(callErr))
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		s.Log.ErrorContext(ctx, "token refresh", slog.String("connection_id", connID.String()), slog.String("error", err.Error()))
		return wrap(err, nil)
	}
	return result
}

// markReconnectRequired flags a connection whose authorization is gone.
func (s *Service) markReconnectRequired(ctx context.Context, q *shopsdb.Queries, tx pgx.Tx, conn shopsdb.ShopConnection, source, code string, meta map[string]any) error {
	if _, err := q.SetConnectionStatus(ctx, shopsdb.SetConnectionStatusParams{Status: StatusReconnectRequired, Now: s.now(), OrgID: conn.OrgID, ID: conn.ID}); err != nil {
		return err
	}
	if err := q.RecordCredentialFailure(ctx, shopsdb.RecordCredentialFailureParams{Code: &code, OrgID: conn.OrgID, ShopConnectionID: conn.ID}); err != nil {
		return err
	}
	if err := s.recordAuthorization(ctx, q, conn.OrgID, conn.ID, "REFRESH_FAILED", source, meta); err != nil {
		return err
	}
	meta["reason"] = code
	if err := s.notifyReconnect(ctx, q, conn, "Shopee không còn chấp nhận ủy quyền của shop này. Hãy kết nối lại shop để quy trình tự động tiếp tục chạy."); err != nil {
		return err
	}
	return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &conn.OrgID, ActorType: audit.ActorSystem, Action: "shop.reconnect_required",
		TargetType: "shop_connection", TargetID: &conn.ID, Metadata: meta})
}

// RefreshPayload is the task payload for one refresh.
type RefreshPayload struct {
	OrgID        uuid.UUID `json:"org_id"`
	ConnectionID uuid.UUID `json:"connection_id"`
}

// HandleRefresh is the worker handler for queue.TypeTokenRefresh.
func (s *Service) HandleRefresh(ctx context.Context, payload []byte) error {
	var p RefreshPayload
	if err := queue.Decode(payload, &p); err != nil {
		return err
	}
	err := s.Refresh(ctx, p.OrgID, p.ConnectionID, scheduledRefreshIn)
	if apperr.IsKind(err, apperr.KindFailedDependency) || apperr.IsKind(err, apperr.KindNotFound) {
		return nil // nothing left to do for this shop
	}
	return err
}

// EnqueueDueRefreshes finds credentials that expire within 30 minutes (using
// the system pool, across tenants) and enqueues one refresh per shop. Task
// IDs include the expiry, so repeated scans do not create duplicates.
func (s *Service) EnqueueDueRefreshes(ctx context.Context) (int, error) {
	if s.System == nil {
		return 0, errors.New("shops: system database is not configured")
	}
	var due []shopsdb.SystemDueRefreshesRow
	err := s.System.Tx(ctx, db.Scope{}, func(tx pgx.Tx) error {
		var err error
		due, err = shopsdb.New(tx).SystemDueRefreshes(ctx, shopsdb.SystemDueRefreshesParams{Before: s.now().Add(scheduledRefreshIn), Lim: 500})
		return err
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range due {
		id := "refresh:" + d.ShopConnectionID.String() + ":" + strconv.FormatInt(d.AccessExpiresAt.Unix(), 10)
		if _, err := s.Queue.Enqueue(ctx, queue.TypeTokenRefresh, RefreshPayload{OrgID: d.OrgID, ConnectionID: d.ShopConnectionID},
			queue.Options{ID: id, MaxRetry: 5, Timeout: time.Minute}); err != nil {
			s.Log.WarnContext(ctx, "enqueue refresh", slog.String("error", err.Error()))
			continue
		}
		n++
	}
	return n, nil
}

// notifyReconnect tells members who can connect shops that a shop needs a
// new authorization.
func (s *Service) notifyReconnect(ctx context.Context, q *shopsdb.Queries, conn shopsdb.ShopConnection, body string) error {
	name := "Shop " + strconv.FormatInt(conn.ShopeeShopID, 10)
	if conn.ShopName != nil && *conn.ShopName != "" {
		name = *conn.ShopName
	}
	return q.NotifyMembers(ctx, shopsdb.NotifyMembersParams{Kind: "shop_reconnect_required", Title: name + ": cần kết nối lại",
		Body: body, Link: "/orgs/" + conn.OrgID.String() + "/shops/" + conn.ID.String(), DedupeKey: "reconnect:" + conn.ID.String(),
		CreatedAt: s.now(), OrgID: conn.OrgID, Roles: rbac.RolesWith(rbac.ShopsConnect), UserIds: []uuid.UUID{}})
}
