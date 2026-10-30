// Package shops owns Shopee partner apps, shop connections, encrypted
// credentials, the OAuth connect flow, token refresh, shop sync, capability
// resolution and the Shopee push (webhook) inbox.
package shops

import (
	"context"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops/shopsdb"
)

// Connection statuses.
const (
	StatusPending           = "PENDING"
	StatusActive            = "ACTIVE"
	StatusReconnectRequired = "RECONNECT_REQUIRED"
	StatusRevoked           = "REVOKED"
	StatusExpired           = "EXPIRED"
	StatusDisconnected      = "DISCONNECTED"
)

const (
	oauthStateTTL      = 10 * time.Minute // matches the authorization code lifetime
	selectionTTL       = 30 * time.Minute
	refreshEarly       = 5 * time.Minute  // just-in-time refresh threshold
	scheduledRefreshIn = 30 * time.Minute // scheduler refresh threshold
	refreshTimeout     = 10 * time.Second
)

// Tenants resolves membership; implemented by tenancy.Service.
type Tenants interface {
	ResolveTenant(ctx context.Context, userID, orgID uuid.UUID) (authctx.Tenant, error)
}

// Service implements the shops use cases.
type Service struct {
	DB *db.DB
	// System is the app_system pool used only for cross-tenant lookups
	// (webhook routing, due refreshes). Nil in processes that do not need it.
	System  *db.DB
	Keys    *envelope.Keyring
	Shopee  *shopee.Client
	Clock   clock.Clock
	Audit   audit.Recorder
	Queue   queue.Enqueuer
	Tenants Tenants
	Log     *slog.Logger
	// BaseURL is the public dashboard URL (redirect targets, default
	// callback URLs).
	BaseURL *url.URL
	// AllowInsecureHosts permits http:// Shopee hosts (local tests only).
	AllowInsecureHosts bool
}

func (s *Service) tx(ctx context.Context, scope db.Scope, fn func(q *shopsdb.Queries, tx pgx.Tx) error) error {
	return s.DB.Tx(ctx, scope, func(tx pgx.Tx) error { return fn(shopsdb.New(tx), tx) })
}

// Shop is the API view of a connection. It never contains tokens.
type Shop struct {
	ID                     uuid.UUID  `json:"id"`
	ShopeeShopID           string     `json:"shopee_shop_id"`
	ShopeeAppID            uuid.UUID  `json:"shopee_app_id"`
	Environment            string     `json:"environment"`
	Region                 *string    `json:"region"`
	ShopName               *string    `json:"shop_name"`
	Status                 string     `json:"connection_status"`
	AuthorizationExpiresAt *time.Time `json:"authorization_expires_at"`
	LastSyncedAt           *time.Time `json:"last_synced_at"`
	LastSyncError          *string    `json:"last_sync_error"`
	ConnectedAt            time.Time  `json:"connected_at"`
	CreatedAt              time.Time  `json:"created_at"`
}

func toShop(c shopsdb.ShopConnection, env string) Shop {
	return Shop{
		ID: c.ID, ShopeeShopID: strconv.FormatInt(c.ShopeeShopID, 10), ShopeeAppID: c.ShopeeAppID,
		Environment: env, Region: c.Region, ShopName: c.ShopName, Status: c.Status,
		AuthorizationExpiresAt: utcPtr(c.AuthorizationExpiresAt), LastSyncedAt: utcPtr(c.LastSyncedAt),
		LastSyncError: c.LastSyncError, ConnectedAt: c.ConnectedAt.UTC(), CreatedAt: c.CreatedAt.UTC(),
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

var (
	errShopNotFound      = apperr.NotFound("Shop not found.")
	errReconnectRequired = apperr.FailedDependency("shop_reconnect_required",
		"This shop's Shopee connection is not active. Reconnect the shop to continue.")
)

// wrap maps "no rows" to notFound and unknown errors to internal errors.
func wrap(err error, notFound *apperr.Error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	if db.IsNoRows(err) && notFound != nil {
		return notFound
	}
	return apperr.Internal(err)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// upstreamError converts a Shopee error for API responses. Integration
// details are only attached for members who manage shops.
func upstreamError(err error, showDetails bool) error {
	e, ok := shopee.AsError(err)
	if !ok {
		return apperr.Internal(err)
	}
	var out *apperr.Error
	switch e.Kind {
	case shopee.KindAuthorizationLost:
		out = errReconnectRequired
		out = &apperr.Error{Kind: out.Kind, Code: out.Code, Message: out.Message}
	case shopee.KindRateLimited, shopee.KindTransient, shopee.KindAmbiguous:
		out = apperr.Unavailable("upstream_unavailable", "Shopee is not responding right now. Try again in a few minutes.")
	default:
		out = apperr.UpstreamFailed("upstream_error", "Shopee rejected the request.")
	}
	out.Err = err
	if showDetails {
		out.Upstream = &apperr.Upstream{Code: e.Code, Message: e.Message, RequestID: e.RequestID}
	}
	return out
}
