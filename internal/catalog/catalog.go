// Package catalog serves Shopee categories, attributes and brands of a shop
// for the product form, cached for a day per shop.
package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/catalog/catalogdb"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/httpx"
	"shopee-automation/internal/rbac"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops"
	"shopee-automation/internal/workflows/shopstep"
)

const (
	ttl           = 24 * time.Hour
	maxBrandPages = 10
)

// Service reads through the cache.
type Service struct {
	DB    *db.DB
	Shops *shops.Service
	Clock clock.Clock
}

// cached returns the cached payload, or calls fetch and stores its result.
func (s *Service) cached(ctx context.Context, p authctx.Principal, t authctx.Tenant, shopID uuid.UUID, kind, key string, refresh bool,
	fetch func(acc shops.Access) (any, error)) (json.RawMessage, error) {
	now := s.Clock.Now()
	var hit json.RawMessage
	err := s.DB.Tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(tx pgx.Tx) error {
		r, err := catalogdb.New(tx).GetCatalog(ctx, catalogdb.GetCatalogParams{OrgID: t.OrgID, ShopConnectionID: shopID, Kind: kind, CacheKey: key})
		if err == nil && !refresh && now.Sub(r.FetchedAt) < ttl {
			hit = r.Payload
		}
		if db.IsNoRows(err) {
			return nil
		}
		return err
	})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if hit != nil {
		return hit, nil
	}
	if _, err := s.Shops.Get(ctx, t, shopID); err != nil {
		return nil, err // 404 for a shop of another org
	}
	var v any
	err = shopstep.WithAccess(ctx, s.Shops, t.OrgID, shopID, func(acc shops.Access) error {
		var err error
		v, err = fetch(acc)
		return err
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return nil, err
		}
		return nil, shops.UpstreamError(err, t.Can(rbac.ShopsConnect))
	}
	payload, _ := json.Marshal(v)
	err = s.DB.Tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(tx pgx.Tx) error {
		return catalogdb.New(tx).PutCatalog(ctx, catalogdb.PutCatalogParams{OrgID: t.OrgID, ShopConnectionID: shopID, Kind: kind, CacheKey: key,
			Payload: payload, FetchedAt: now})
	})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return payload, nil
}

// Categories returns the shop's category tree as a flat list.
func (s *Service) Categories(ctx context.Context, p authctx.Principal, t authctx.Tenant, shopID uuid.UUID, refresh bool) (json.RawMessage, error) {
	return s.cached(ctx, p, t, shopID, "categories", "", refresh, func(acc shops.Access) (any, error) {
		list, err := s.Shops.Shopee.GetCategories(ctx, acc.App, acc.AccessToken, acc.ShopID)
		if list == nil {
			list = []shopee.Category{}
		}
		return list, err
	})
}

// Attributes returns the attribute tree of a category.
func (s *Service) Attributes(ctx context.Context, p authctx.Principal, t authctx.Tenant, shopID uuid.UUID, categoryID int64) (json.RawMessage, error) {
	return s.cached(ctx, p, t, shopID, "attributes", strconv.FormatInt(categoryID, 10), false, func(acc shops.Access) (any, error) {
		tree, err := s.Shops.Shopee.GetAttributeTree(ctx, acc.App, acc.AccessToken, acc.ShopID, []int64{categoryID})
		list := tree[categoryID]
		if list == nil {
			list = []shopee.Attribute{}
		}
		return list, err
	})
}

// Brands returns the brand list of a category (first 10 pages).
func (s *Service) Brands(ctx context.Context, p authctx.Principal, t authctx.Tenant, shopID uuid.UUID, categoryID int64) (json.RawMessage, error) {
	return s.cached(ctx, p, t, shopID, "brands", strconv.FormatInt(categoryID, 10), false, func(acc shops.Access) (any, error) {
		out := shopee.BrandPage{Brands: []shopee.Brand{}}
		var offset int64
		for range maxBrandPages {
			bp, err := s.Shops.Shopee.GetBrandList(ctx, acc.App, acc.AccessToken, acc.ShopID, categoryID, offset)
			if err != nil {
				return nil, err
			}
			out.IsMandatory = bp.IsMandatory
			out.Brands = append(out.Brands, bp.Brands...)
			if !bp.HasNextPage {
				break
			}
			offset = bp.NextOffset
		}
		return out, nil
	})
}

// Handler serves the catalog endpoints.
type Handler struct{ Svc *Service }

func params(r *http.Request, withCategory bool) (uuid.UUID, int64, error) {
	id, err := uuid.Parse(chi.URLParam(r, "shopID"))
	if err != nil {
		return uuid.Nil, 0, apperr.NotFound("Shop not found.")
	}
	if !withCategory {
		return id, 0, nil
	}
	c, err := strconv.ParseInt(chi.URLParam(r, "categoryID"), 10, 64)
	if err != nil || c <= 0 {
		return uuid.Nil, 0, apperr.NotFound("Category not found.")
	}
	return id, c, nil
}

func write(w http.ResponseWriter, raw json.RawMessage, err error) error {
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[json.RawMessage]{Data: raw})
}

// Categories handles GET /orgs/{orgID}/shops/{shopID}/catalog/categories.
func (h Handler) Categories(w http.ResponseWriter, r *http.Request) error {
	id, _, err := params(r, false)
	if err != nil {
		return err
	}
	raw, err := h.Svc.Categories(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id, r.URL.Query().Get("refresh") == "true")
	return write(w, raw, err)
}

// Attributes handles GET /orgs/{orgID}/shops/{shopID}/catalog/categories/{categoryID}/attributes.
func (h Handler) Attributes(w http.ResponseWriter, r *http.Request) error {
	id, c, err := params(r, true)
	if err != nil {
		return err
	}
	raw, err := h.Svc.Attributes(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id, c)
	return write(w, raw, err)
}

// Brands handles GET /orgs/{orgID}/shops/{shopID}/catalog/categories/{categoryID}/brands.
func (h Handler) Brands(w http.ResponseWriter, r *http.Request) error {
	id, c, err := params(r, true)
	if err != nil {
		return err
	}
	raw, err := h.Svc.Brands(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id, c)
	return write(w, raw, err)
}
