package shops

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/httpx"
)

// Handler serves shop, partner-app and webhook endpoints.
type Handler struct {
	Svc *Service
	Log *slog.Logger
}

func pathUUID(r *http.Request, name string, notFound error) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, notFound
	}
	return id, nil
}

func optional(r *http.Request, key string) *string {
	if v := r.URL.Query().Get(key); v != "" {
		return &v
	}
	return nil
}

// List handles GET /orgs/{orgID}/shops.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.List(r.Context(), authctx.MustTenant(r.Context()),
		ListFilter{Status: optional(r, "status"), Region: optional(r, "region"), Q: optional(r, "q")})
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[Shop]{Data: out})
}

// Get handles GET /orgs/{orgID}/shops/{shopID}.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "shopID", errShopNotFound)
	if err != nil {
		return err
	}
	d, err := h.Svc.Get(r.Context(), authctx.MustTenant(r.Context()), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Detail]{Data: d})
}

// Capabilities handles GET /orgs/{orgID}/shops/{shopID}/capabilities.
func (h Handler) Capabilities(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "shopID", errShopNotFound)
	if err != nil {
		return err
	}
	out, err := h.Svc.Capabilities(r.Context(), authctx.MustTenant(r.Context()), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[any]{Data: toAny(out)})
}

func toAny[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// Connect handles POST /orgs/{orgID}/shops/connect.
func (h Handler) Connect(w http.ResponseWriter, r *http.Request) error {
	var in ConnectInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	res, err := h.Svc.Connect(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[ConnectResult]{Data: res})
}

// Reconnect handles POST /orgs/{orgID}/shops/{shopID}/reconnect.
func (h Handler) Reconnect(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "shopID", errShopNotFound)
	if err != nil {
		return err
	}
	res, err := h.Svc.Reconnect(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[ConnectResult]{Data: res})
}

var errSelectionNotFound = apperr.NotFound("Shop selection not found or expired.")

// GetSelection handles GET /orgs/{orgID}/shops/connect/{stateID}.
func (h Handler) GetSelection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "stateID", errSelectionNotFound)
	if err != nil {
		return err
	}
	sel, err := h.Svc.GetSelection(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Selection]{Data: sel})
}

// SelectShops handles POST /orgs/{orgID}/shops/connect/{stateID}/select.
func (h Handler) SelectShops(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "stateID", errSelectionNotFound)
	if err != nil {
		return err
	}
	var in SelectInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	out, err := h.Svc.SelectShops(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id, in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.List[Shop]{Data: out})
}

// Sync handles POST /orgs/{orgID}/shops/{shopID}/sync.
func (h Handler) Sync(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "shopID", errShopNotFound)
	if err != nil {
		return err
	}
	job, err := h.Svc.RequestSync(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[SyncJob]{Data: job})
}

// Disconnect handles DELETE /orgs/{orgID}/shops/{shopID}.
func (h Handler) Disconnect(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "shopID", errShopNotFound)
	if err != nil {
		return err
	}
	var in DisconnectInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.Svc.Disconnect(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id, in); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// Callback handles GET /oauth/shopee/callback, the redirect target that
// Shopee sends the seller back to. It always answers with a redirect.
func (h Handler) Callback(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := authctx.PrincipalFrom(r.Context())
	if !ok {
		// Keep the query: the state stays valid until it is consumed, so the
		// user can sign in and come back within the 10-minute window.
		next := r.URL.Path + "?" + r.URL.RawQuery
		http.Redirect(w, r, h.Svc.dashboardURL("/login", url.Values{"next": {next}}), http.StatusFound)
		return nil
	}
	q := r.URL.Query()
	target := h.Svc.Callback(r.Context(), p, CallbackInput{
		Code: q.Get("code"), ShopID: q.Get("shop_id"), MainAccountID: q.Get("main_account_id"), State: q.Get("state"),
	})
	http.Redirect(w, r, target, http.StatusFound)
	return nil
}

// Webhook handles POST /webhooks/shopee/{appID}. Success is an empty 204,
// as Shopee expects a fast 2xx without a body.
func (h Handler) Webhook(w http.ResponseWriter, r *http.Request) error {
	id, err := pathUUID(r, "appID", apperr.NotFound("Route not found."))
	if err != nil {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBody+1))
	if err != nil {
		return apperr.BadRequest("bad_request", "Could not read body.")
	}
	if len(body) > MaxWebhookBody {
		return &apperr.Error{Kind: apperr.KindPayloadTooLarge, Code: "payload_too_large", Message: "Body is too large."}
	}
	if err := h.Svc.ReceiveWebhook(r.Context(), id, body, r.Header.Get("Authorization")); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// ListApps handles GET /admin/shopee-apps.
func (h Handler) ListApps(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.ListApps(r.Context(), authctx.MustPrincipal(r.Context()))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[App]{Data: out})
}

// CreateApp handles POST /admin/shopee-apps.
func (h Handler) CreateApp(w http.ResponseWriter, r *http.Request) error {
	if err := requireAdmin(authctx.MustPrincipal(r.Context())); err != nil {
		return err
	}
	var in CreateAppInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	a, err := h.Svc.CreateApp(r.Context(), authctx.MustPrincipal(r.Context()), in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.Data[App]{Data: a})
}

// UpdateApp handles PATCH /admin/shopee-apps/{appID}.
func (h Handler) UpdateApp(w http.ResponseWriter, r *http.Request) error {
	p := authctx.MustPrincipal(r.Context())
	if err := requireAdmin(p); err != nil {
		return err
	}
	id, err := pathUUID(r, "appID", apperr.NotFound("Shopee app not found."))
	if err != nil {
		return err
	}
	var in UpdateAppInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	a, err := h.Svc.UpdateApp(r.Context(), p, id, in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[App]{Data: a})
}
