package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/httpx"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/platform/reqmeta"
)

// Handler serves the dashboard, admin and extension endpoints.
type Handler struct {
	Svc     *Service
	Limiter ratelimit.Limiter
	Log     *slog.Logger
	Adapter httpx.Adapter
	// Origins are the extension origins allowed by CORS
	// (chrome-extension://<id>).
	Origins []string
}

type deviceKey struct{}

// DeviceFrom returns the authenticated device.
func DeviceFrom(ctx context.Context) (Device, bool) {
	d, ok := ctx.Value(deviceKey{}).(Device)
	return d, ok
}

func mustDevice(r *http.Request) Device {
	d, ok := DeviceFrom(r.Context())
	if !ok {
		panic("browser: route without device middleware")
	}
	return d
}

// CORS answers preflights and allows only the configured extension
// origins. Requests without an Origin header (tests, curl) pass.
func (h Handler) CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if !slices.Contains(h.Origins, origin) {
				h.Adapter.WriteError(w, r, apperr.Forbidden("origin_not_allowed", "This origin may not call the extension API."))
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Extension-Version")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireDevice authenticates the bearer token.
func (h Handler) RequireDevice(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok {
			h.Adapter.WriteError(w, r, ErrDeviceUnauthorized)
			return
		}
		d, err := h.Svc.Authenticate(r.Context(), strings.TrimSpace(token), r.Header.Get("X-Extension-Version"))
		if err != nil {
			h.Adapter.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), deviceKey{}, d)))
	})
}

func (h Handler) limit(r *http.Request, key string, n int, window time.Duration) error {
	ok, retry, err := h.Limiter.Allow(r.Context(), key, n, window)
	if err != nil {
		h.Log.WarnContext(r.Context(), "rate limiter unavailable", slog.String("error", err.Error()))
		return nil
	}
	if !ok {
		return apperr.RateLimited(fmt.Sprintf("Too many attempts. Try again in %d seconds.", int(retry.Seconds())+1))
	}
	return nil
}

func idParam(r *http.Request, name, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, apperr.NotFound(what + " not found.")
	}
	return id, nil
}

// --- Dashboard ---

// CreatePairingCode handles POST /orgs/{orgID}/extension/pairing-codes.
func (h Handler) CreatePairingCode(w http.ResponseWriter, r *http.Request) error {
	p := authctx.MustPrincipal(r.Context())
	if err := h.limit(r, "ext-code:user:"+p.UserID.String(), 10, time.Hour); err != nil {
		return err
	}
	out, err := h.Svc.CreatePairingCode(r.Context(), p, authctx.MustTenant(r.Context()))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return httpx.JSON(w, http.StatusCreated, httpx.Data[PairingCode]{Data: out})
}

// ListDevices handles GET /orgs/{orgID}/extension/devices.
func (h Handler) ListDevices(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.ListDevices(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[[]DeviceInfo]{Data: out})
}

// RevokeDevice handles DELETE /orgs/{orgID}/extension/devices/{deviceID}.
func (h Handler) RevokeDevice(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "deviceID", "Device")
	if err != nil {
		return err
	}
	if err := h.Svc.Revoke(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// RunTasks handles GET /orgs/{orgID}/workflows/{runID}/browser-tasks.
func (h Handler) RunTasks(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "runID", "Run")
	if err != nil {
		return err
	}
	out, err := h.Svc.ListForRun(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[[]TaskView]{Data: out})
}

// Requeue handles POST /orgs/{orgID}/browser-tasks/{taskID}/requeue.
func (h Handler) Requeue(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "taskID", "Task")
	if err != nil {
		return err
	}
	if err := h.Svc.Requeue(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), id); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// Screenshot handles GET /orgs/{orgID}/browser-tasks/{taskID}/screenshots/{screenshotID}.
func (h Handler) Screenshot(w http.ResponseWriter, r *http.Request) error {
	tid, err := idParam(r, "taskID", "Task")
	if err != nil {
		return err
	}
	sid, err := idParam(r, "screenshotID", "Screenshot")
	if err != nil {
		return err
	}
	u, exp, err := h.Svc.ScreenshotURL(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), tid, sid)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[map[string]any]{Data: map[string]any{"url": u, "expires_at": exp}})
}

// --- Platform admin ---

// ListRecipes handles GET /admin/browser-recipes.
func (h Handler) ListRecipes(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.ListRecipes(r.Context(), authctx.MustPrincipal(r.Context()))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[[]RecipeView]{Data: out})
}

// CreateRecipe handles POST /admin/browser-recipes.
func (h Handler) CreateRecipe(w http.ResponseWriter, r *http.Request) error {
	var in CreateRecipeInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	out, err := h.Svc.CreateRecipe(r.Context(), authctx.MustPrincipal(r.Context()), in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.Data[RecipeView]{Data: out})
}

// UpdateRecipe handles PATCH /admin/browser-recipes/{recipeID}.
func (h Handler) UpdateRecipe(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "recipeID", "Recipe")
	if err != nil {
		return err
	}
	var in UpdateRecipeInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	out, err := h.Svc.UpdateRecipe(r.Context(), authctx.MustPrincipal(r.Context()), id, in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[RecipeView]{Data: out})
}

// --- Extension ---

func clientIP(r *http.Request) string {
	if ip := reqmeta.From(r.Context()).IP; ip != nil {
		return ip.String()
	}
	return "unknown"
}

// Pair handles POST /ext/pair.
func (h Handler) Pair(w http.ResponseWriter, r *http.Request) error {
	if err := h.limit(r, "ext-pair:ip:"+clientIP(r), 10, 10*time.Minute); err != nil {
		return err
	}
	var in PairInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	out, err := h.Svc.Pair(r.Context(), in)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return httpx.JSON(w, http.StatusCreated, httpx.Data[Paired]{Data: out})
}

// Me handles GET /ext/me.
func (h Handler) Me(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Me(r.Context(), mustDevice(r))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Me]{Data: out})
}

// Rotate handles POST /ext/token/rotate.
func (h Handler) Rotate(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.Rotate(r.Context(), mustDevice(r))
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return httpx.JSON(w, http.StatusOK, httpx.Data[Paired]{Data: out})
}

// Unpair handles DELETE /ext/device.
func (h Handler) Unpair(w http.ResponseWriter, r *http.Request) error {
	d := mustDevice(r)
	if err := h.Svc.Revoke(r.Context(), d.Principal(), d.Tenant(), d.ID); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// Tasks handles GET /ext/tasks.
func (h Handler) Tasks(w http.ResponseWriter, r *http.Request) error {
	out, err := h.Svc.ListForDevice(r.Context(), mustDevice(r))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[[]TaskView]{Data: out})
}

// Claim handles POST /ext/tasks/{taskID}/claim.
func (h Handler) Claim(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "taskID", "Task")
	if err != nil {
		return err
	}
	out, err := h.Svc.Claim(r.Context(), mustDevice(r), id)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return httpx.JSON(w, http.StatusOK, httpx.Data[ClaimResult]{Data: out})
}

// Heartbeat handles POST /ext/tasks/{taskID}/heartbeat.
func (h Handler) Heartbeat(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "taskID", "Task")
	if err != nil {
		return err
	}
	var in HeartbeatInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.Svc.Heartbeat(r.Context(), mustDevice(r), id, in); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// AddScreenshot handles POST /ext/tasks/{taskID}/screenshots (multipart).
func (h Handler) AddScreenshot(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "taskID", "Task")
	if err != nil {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxScreenshotSize+1<<20)
	if err := r.ParseMultipartForm(maxScreenshotSize + 1<<20); err != nil { //nolint:gosec // G120: body is bounded by MaxBytesReader above
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return &apperr.Error{Kind: apperr.KindPayloadTooLarge, Code: "payload_too_large", Message: "Screenshots are limited to 5 MB."}
		}
		return &apperr.Error{Kind: apperr.KindUnsupportedMediaType, Code: "unsupported_media_type", Message: "Send multipart/form-data with a file field."}
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	lease, err := uuid.Parse(r.FormValue("lease_token"))
	if err != nil {
		return apperr.Invalid(apperr.FieldError{Field: "lease_token", Code: "invalid", Message: "Must be the task's lease token."})
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		return apperr.Invalid(apperr.FieldError{Field: "file", Code: "required", Message: "Attach the screenshot as file."})
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxScreenshotSize+1))
	if err != nil {
		return apperr.Internal(err)
	}
	sid, err := h.Svc.AddScreenshot(r.Context(), mustDevice(r), id, lease, data)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.Data[map[string]uuid.UUID]{Data: map[string]uuid.UUID{"id": sid}})
}

// Result handles POST /ext/tasks/{taskID}/result.
func (h Handler) Result(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "taskID", "Task")
	if err != nil {
		return err
	}
	var in ResultInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	if err := h.Svc.Result(r.Context(), mustDevice(r), id, in); err != nil {
		return err
	}
	return httpx.NoContent(w)
}
