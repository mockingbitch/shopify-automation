// Package overview serves the dashboard's summary numbers and the per-shop
// activity panel (configuration status, published products, history).
package overview

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/overview/overviewdb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/httpx"
)

// Service reads counts in the tenant scope.
type Service struct {
	DB    *db.DB
	Clock clock.Clock
}

// Counts are the overview numbers.
type Counts struct {
	ConnectedShops     int64 `json:"connected_shops"`
	ActiveShops        int64 `json:"active_shops"`
	DisconnectedShops  int64 `json:"disconnected_shops"`
	ReconnectRequired  int64 `json:"reconnect_required"`
	RunningWorkflows   int64 `json:"running_workflows"`
	WaitingWorkflows   int64 `json:"waiting_workflows"`
	FailedWorkflows    int64 `json:"failed_workflows_30d"`
	CompletedSetups    int64 `json:"completed_setups"`
	PendingApprovals   int64 `json:"pending_approvals"`
	QueuedBrowserTasks int64 `json:"queued_browser_tasks"`
	Products           int64 `json:"products"`
	PublishedProducts  int64 `json:"published_products"`
}

// RunRef is a short run summary.
type RunRef struct {
	ID         uuid.UUID       `json:"id"`
	Definition string          `json:"definition"`
	Status     string          `json:"status"`
	Input      json.RawMessage `json:"input"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at"`
}

// ShopActivity is the shop detail panel.
type ShopActivity struct {
	// LastSetup is the newest SHOP_SETUP run (configuration status).
	LastSetup                *RunRef  `json:"last_setup"`
	PublishedProducts        int64    `json:"published_products"`
	ProductsNeedingAttention int64    `json:"products_needing_attention"`
	Runs                     []RunRef `json:"runs"`
}

func (s *Service) tx(ctx context.Context, p authctx.Principal, t authctx.Tenant, fn func(q *overviewdb.Queries) error) error {
	return s.DB.Tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(tx pgx.Tx) error { return fn(overviewdb.New(tx)) })
}

// Org returns the overview numbers.
func (s *Service) Org(ctx context.Context, p authctx.Principal, t authctx.Tenant) (Counts, error) {
	var c Counts
	err := s.tx(ctx, p, t, func(q *overviewdb.Queries) error {
		r, err := q.OrgCounts(ctx, overviewdb.OrgCountsParams{OrgID: t.OrgID, Since: s.Clock.Now().Add(-30 * 24 * time.Hour)})
		c = Counts(r)
		return err
	})
	if err != nil {
		return Counts{}, apperr.Internal(err)
	}
	return c, nil
}

// Shop returns one shop's activity.
func (s *Service) Shop(ctx context.Context, p authctx.Principal, t authctx.Tenant, shopID uuid.UUID) (ShopActivity, error) {
	out := ShopActivity{Runs: []RunRef{}}
	err := s.tx(ctx, p, t, func(q *overviewdb.Queries) error {
		ok, err := q.ShopExists(ctx, overviewdb.ShopExistsParams{OrgID: t.OrgID, ID: shopID})
		if err != nil {
			return err
		}
		if !ok {
			return apperr.NotFound("Shop not found.")
		}
		c, err := q.ShopCounts(ctx, overviewdb.ShopCountsParams{OrgID: t.OrgID, ShopID: shopID})
		if err != nil {
			return err
		}
		out.PublishedProducts, out.ProductsNeedingAttention = c.PublishedProducts, c.ProductsNeedingAttention
		if ls, err := q.LastSetup(ctx, overviewdb.LastSetupParams{OrgID: t.OrgID, ShopID: shopID}); err == nil {
			out.LastSetup = &RunRef{ID: ls.ID, Definition: "SHOP_SETUP", Status: ls.Status, Input: ls.Input, CreatedAt: ls.CreatedAt.UTC(), FinishedAt: ls.FinishedAt}
		} else if !db.IsNoRows(err) {
			return err
		}
		runs, err := q.ShopRuns(ctx, overviewdb.ShopRunsParams{OrgID: t.OrgID, ShopID: shopID})
		for _, r := range runs {
			out.Runs = append(out.Runs, RunRef{ID: r.ID, Definition: r.DefinitionKey, Status: r.Status, Input: r.Input, CreatedAt: r.CreatedAt.UTC(), FinishedAt: r.FinishedAt})
		}
		return err
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return ShopActivity{}, err
		}
		return ShopActivity{}, apperr.Internal(err)
	}
	return out, nil
}

// Handler serves the overview endpoints.
type Handler struct{ Svc *Service }

// Org handles GET /orgs/{orgID}/overview.
func (h Handler) Org(w http.ResponseWriter, r *http.Request) error {
	c, err := h.Svc.Org(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Counts]{Data: c})
}

// Shop handles GET /orgs/{orgID}/shops/{shopID}/activity.
func (h Handler) Shop(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(chi.URLParam(r, "shopID"))
	if err != nil {
		return apperr.NotFound("Shop not found.")
	}
	a, err := h.Svc.Shop(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[ShopActivity]{Data: a})
}
