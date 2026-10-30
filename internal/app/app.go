// Package app wires modules into the HTTP API. The route table is data, so
// tests can check that every route declares its access rule and that the
// OpenAPI document lists exactly these routes.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/browser"
	"shopee-automation/internal/catalog"
	"shopee-automation/internal/identity"
	"shopee-automation/internal/notify"
	"shopee-automation/internal/overview"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/config"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/envelope"
	"shopee-automation/internal/platform/httpx"
	"shopee-automation/internal/platform/mailer"
	"shopee-automation/internal/platform/media"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/platform/ratelimit"
	"shopee-automation/internal/platform/storage"
	"shopee-automation/internal/products"
	"shopee-automation/internal/rbac"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops"
	"shopee-automation/internal/templates"
	"shopee-automation/internal/tenancy"
	"shopee-automation/internal/workflow"
	"shopee-automation/internal/workflow/workflowdb"
	"shopee-automation/internal/workflows/productpublish"
	"shopee-automation/internal/workflows/shopsetup"
)

// Access describes who may call a route.
type Access int

const (
	// Public routes need no session.
	Public Access = iota
	// Session routes need a signed-in user (and CSRF on unsafe methods).
	Session
	// Tenant routes also resolve /orgs/{orgID} membership and check Perm.
	Tenant
	// TenantSelf resolves membership but leaves the permission check to the
	// handler (e.g. a member removing themselves).
	TenantSelf
	// Admin routes need a platform admin; everyone else gets 404.
	Admin
	// Callback is a browser redirect target: the session is loaded when
	// present, but the handler decides what to do without one (no CSRF,
	// it is a top-level GET navigation).
	Callback
	// Webhook routes are called by Shopee. No session, no CSRF, no origin
	// check; the handler verifies the request signature.
	Webhook
	// Extension routes are called by the browser extension with a device
	// token (no cookies, no CSRF); CORS allows only extension origins.
	Extension
	// ExtensionPublic is the pairing route: CORS but no device token.
	ExtensionPublic
)

// Route is one API endpoint under /api/v1.
type Route struct {
	Method  string
	Path    string
	Access  Access
	Perm    rbac.Permission
	Handler httpx.HandlerFunc
}

// Deps are the external dependencies of the API.
type Deps struct {
	Config  config.Config
	DB      *db.DB
	Redis   *redis.Client // optional; used for readiness and rate limits
	Limiter ratelimit.Limiter
	Mailer  mailer.Mailer
	Clock   clock.Clock
	Log     *slog.Logger
	Hasher  identity.Hasher

	// SystemDB is the app_system pool for cross-tenant lookups (optional in
	// the API; webhook processing runs on the worker).
	SystemDB *db.DB
	Keys     *envelope.Keyring
	Shopee   *shopee.Client
	Queue    queue.Enqueuer
	Store    storage.Store
	Fetcher  *media.Fetcher
	// Signer signs browser recipes; nil disables task claims.
	Signer *browser.Signer
}

// App is the assembled API.
type App struct {
	deps     Deps
	adapter  httpx.Adapter
	identity identity.Handler
	tenancy  tenancy.Handler
	audit    audit.Handler
	shops    shops.Handler
	tpl      templates.Handler
	wf       workflow.Handler
	prod     products.Handler
	// Products is exposed for the worker and tests.
	Products *products.Service
	apply    shopsetup.Handler
	publish  productpublish.Handler
	// Publish is exposed so tests can shorten the variation delay.
	Publish  *productpublish.Workflow
	browser  browser.Handler
	notes    notify.Handler
	overview overview.Handler
	catalog  catalog.Handler
	// Outbox sends email from a retried task (nil without a keyring).
	Outbox *notify.Outbox
	// Browser is exposed for tests.
	Browser *browser.Service
	// Engine is exposed for the worker and tests.
	Engine *workflow.Engine
	// Shops is exposed for the worker and tests.
	Shops *shops.Service
}

// New assembles the application.
func New(d Deps) *App {
	rec := audit.Recorder{Clock: d.Clock}
	var outbox *notify.Outbox
	if d.Keys != nil && d.Queue != nil {
		outbox = &notify.Outbox{DB: d.DB, System: d.SystemDB, Keys: d.Keys, Queue: d.Queue, Mailer: d.Mailer, Clock: d.Clock, Log: d.Log}
		d.Mailer = outbox
	}
	idSvc := &identity.Service{
		DB: d.DB, Clock: d.Clock, Mailer: d.Mailer, Audit: rec, Log: d.Log, Hasher: d.Hasher,
		BaseURL: d.Config.PublicBaseURL, IdleTTL: d.Config.SessionIdleTTL, AbsoluteTTL: d.Config.SessionAbsoluteTTL,
	}
	tSvc := &tenancy.Service{DB: d.DB, Clock: d.Clock, Audit: rec, Mailer: d.Mailer, Log: d.Log, BaseURL: d.Config.PublicBaseURL}
	a := &App{
		deps:    d,
		adapter: httpx.Adapter{Log: d.Log},
		tenancy: tenancy.Handler{Svc: tSvc, Log: d.Log},
		audit:   audit.Handler{DB: d.DB},
	}
	a.Shops = &shops.Service{
		DB: d.DB, System: d.SystemDB, Keys: d.Keys, Shopee: d.Shopee, Clock: d.Clock, Audit: rec,
		Queue: d.Queue, Tenants: tSvc, Log: d.Log, BaseURL: d.Config.PublicBaseURL,
		AllowInsecureHosts: !d.Config.IsProduction(),
	}
	a.shops = shops.Handler{Svc: a.Shops, Log: d.Log}
	a.Engine = &workflow.Engine{DB: d.DB, System: d.SystemDB, Queue: d.Queue, Clock: d.Clock, Audit: rec, Log: d.Log}
	tplSvc := &templates.Service{DB: d.DB, Clock: d.Clock, Audit: rec, Registry: templateRegistry, Shops: a.Shops, Log: d.Log,
		ActiveRuns: func(ctx context.Context, orgID, templateID uuid.UUID) (int64, error) {
			var n int64
			err := d.DB.Tx(ctx, db.Scope{OrgID: orgID}, func(tx pgx.Tx) error {
				var err error
				n, err = workflowdb.New(tx).ActiveRunsForTemplate(ctx, workflowdb.ActiveRunsForTemplateParams{OrgID: orgID, TemplateID: templateID.String()})
				return err
			})
			return n, err
		}}
	a.tpl = templates.Handler{Svc: tplSvc}
	a.Products = &products.Service{DB: d.DB, Clock: d.Clock, Audit: rec, Store: d.Store, Fetcher: d.Fetcher, Queue: d.Queue,
		Log: d.Log, Templates: tplSvc}
	a.prod = products.Handler{Svc: a.Products}
	a.Browser = &browser.Service{DB: d.DB, System: d.SystemDB, Clock: d.Clock, Audit: rec, Store: d.Store, Fetcher: d.Fetcher,
		Signer: d.Signer, Steps: a.Engine, Log: d.Log}
	a.Shops.Extension = a.Browser
	a.Outbox = outbox
	a.notes = notify.Handler{Svc: &notify.Service{DB: d.DB, Clock: d.Clock}}
	a.overview = overview.Handler{Svc: &overview.Service{DB: d.DB, Clock: d.Clock}}
	a.catalog = catalog.Handler{Svc: &catalog.Service{DB: d.DB, Shops: a.Shops, Clock: d.Clock}}
	a.browser = browser.Handler{Svc: a.Browser, Limiter: d.Limiter, Log: d.Log, Adapter: a.adapter, Origins: d.Config.ExtensionOrigins}
	setup := &shopsetup.Setup{Shops: a.Shops, Templates: tplSvc, Engine: a.Engine, Browser: a.Browser}
	a.Engine.Register(setup.Definition())
	a.apply = shopsetup.Handler{Setup: setup, Audit: rec}
	a.Publish = &productpublish.Workflow{Shops: a.Shops, Products: a.Products, Engine: a.Engine, VariationDelay: 5 * time.Second}
	a.Engine.Register(a.Publish.Definition())
	a.publish = productpublish.Handler{Workflow: a.Publish, Audit: rec}
	a.wf = workflow.Handler{Engine: a.Engine, Policy: approvalPolicy(tSvc)}
	a.identity = identity.Handler{
		Svc: idSvc, Limiter: d.Limiter, Log: d.Log, CookieSecure: d.Config.CookieSecure,
		Memberships: func(ctx context.Context, userID uuid.UUID) (any, error) {
			return tSvc.ListMemberships(ctx, userID)
		},
	}
	return a
}

// templateRegistry compiles the embedded section schemas once per process.
var templateRegistry = templates.LoadRegistry()

// Routes returns the API route table.
func (a *App) Routes() []Route {
	id, tn, sh, tp, wf, pr, bx := a.identity, a.tenancy, a.shops, a.tpl, a.wf, a.prod, a.browser
	return []Route{
		{"POST", "/auth/register", Public, "", id.Register},
		{"POST", "/auth/login", Public, "", id.Login},
		{"POST", "/auth/verify-email", Public, "", id.VerifyEmail},
		{"POST", "/auth/password/forgot", Public, "", id.ForgotPassword},
		{"POST", "/auth/password/reset", Public, "", id.ResetPassword},
		{"POST", "/auth/logout", Session, "", id.Logout},
		{"POST", "/auth/verify-email/resend", Session, "", id.ResendVerification},
		{"GET", "/auth/session", Session, "", id.Session},
		{"GET", "/me", Session, "", id.Me},
		{"PATCH", "/me", Session, "", id.UpdateMe},

		{"POST", "/orgs", Session, "", tn.CreateOrg},
		{"GET", "/orgs", Session, "", tn.ListOrgs},
		{"POST", "/invitations/accept", Session, "", tn.AcceptInvitation},

		{"GET", "/orgs/{orgID}", Tenant, rbac.OrgRead, tn.GetOrg},
		{"PATCH", "/orgs/{orgID}", Tenant, rbac.OrgUpdate, tn.UpdateOrg},
		{"DELETE", "/orgs/{orgID}", Tenant, rbac.OrgDelete, tn.DeleteOrg},
		{"POST", "/orgs/{orgID}/transfer-ownership", Tenant, rbac.OrgTransfer, tn.TransferOwnership},
		{"GET", "/orgs/{orgID}/members", Tenant, rbac.MembersRead, tn.ListMembers},
		{"PATCH", "/orgs/{orgID}/members/{memberID}", Tenant, rbac.MembersManage, tn.ChangeRole},
		{"DELETE", "/orgs/{orgID}/members/{memberID}", TenantSelf, "", tn.RemoveMember},
		{"POST", "/orgs/{orgID}/invitations", Tenant, rbac.MembersManage, tn.Invite},
		{"GET", "/orgs/{orgID}/invitations", Tenant, rbac.MembersManage, tn.ListInvitations},
		{"DELETE", "/orgs/{orgID}/invitations/{invitationID}", Tenant, rbac.MembersManage, tn.RevokeInvitation},
		{"GET", "/orgs/{orgID}/audit-logs", Tenant, rbac.AuditRead, a.audit.List},

		{"POST", "/orgs/{orgID}/shops/connect", Tenant, rbac.ShopsConnect, sh.Connect},
		{"GET", "/orgs/{orgID}/shops/connect/{stateID}", Tenant, rbac.ShopsConnect, sh.GetSelection},
		{"POST", "/orgs/{orgID}/shops/connect/{stateID}/select", Tenant, rbac.ShopsConnect, sh.SelectShops},
		{"GET", "/orgs/{orgID}/shops", Tenant, rbac.ShopsRead, sh.List},
		{"GET", "/orgs/{orgID}/shops/{shopID}", Tenant, rbac.ShopsRead, sh.Get},
		{"GET", "/orgs/{orgID}/shops/{shopID}/capabilities", Tenant, rbac.ShopsRead, sh.Capabilities},
		{"POST", "/orgs/{orgID}/shops/{shopID}/sync", Tenant, rbac.ShopsSync, sh.Sync},
		{"POST", "/orgs/{orgID}/shops/{shopID}/reconnect", Tenant, rbac.ShopsConnect, sh.Reconnect},
		{"DELETE", "/orgs/{orgID}/shops/{shopID}", Tenant, rbac.ShopsDisconnect, sh.Disconnect},
		{"GET", "/oauth/shopee/callback", Callback, "", sh.Callback},
		{"POST", "/webhooks/shopee/{appID}", Webhook, "", sh.Webhook},

		{"GET", "/template-sections", Session, "", tp.Schemas},
		{"GET", "/orgs/{orgID}/templates", Tenant, rbac.TemplatesRead, tp.List},
		{"POST", "/orgs/{orgID}/templates", Tenant, rbac.TemplatesWrite, tp.Create},
		{"GET", "/orgs/{orgID}/templates/{templateID}", Tenant, rbac.TemplatesRead, tp.Get},
		{"PATCH", "/orgs/{orgID}/templates/{templateID}", Tenant, rbac.TemplatesWrite, tp.Update},
		{"DELETE", "/orgs/{orgID}/templates/{templateID}", Tenant, rbac.TemplatesPublish, tp.Delete},
		{"POST", "/orgs/{orgID}/templates/{templateID}/clone", Tenant, rbac.TemplatesWrite, tp.Clone},
		{"POST", "/orgs/{orgID}/templates/{templateID}/versions", Tenant, rbac.TemplatesWrite, tp.NewDraft},
		{"GET", "/orgs/{orgID}/templates/{templateID}/versions/{versionID}", Tenant, rbac.TemplatesRead, tp.GetVersion},
		{"PUT", "/orgs/{orgID}/templates/{templateID}/versions/{versionID}/sections/{sectionKey}", Tenant, rbac.TemplatesWrite, tp.PutSection},
		{"POST", "/orgs/{orgID}/templates/{templateID}/versions/{versionID}/validate", Tenant, rbac.TemplatesRead, tp.Validate},
		{"POST", "/orgs/{orgID}/templates/{templateID}/versions/{versionID}/publish", Tenant, rbac.TemplatesPublish, tp.Publish},
		{"POST", "/orgs/{orgID}/templates/{templateID}/versions/{versionID}/preview", Tenant, rbac.TemplatesRead, tp.Preview},
		{"GET", "/orgs/{orgID}/templates/{templateID}/shops/{shopID}/overrides", Tenant, rbac.TemplatesRead, tp.GetOverrides},
		{"PUT", "/orgs/{orgID}/templates/{templateID}/shops/{shopID}/overrides", Tenant, rbac.TemplatesWrite, tp.PutOverrides},
		{"POST", "/orgs/{orgID}/templates/{templateID}/versions/{versionID}/apply", Tenant, rbac.WorkflowsRun, a.apply.Apply},

		{"GET", "/orgs/{orgID}/workflows", Tenant, rbac.WorkflowsRead, wf.List},
		{"GET", "/orgs/{orgID}/workflows/{runID}", Tenant, rbac.WorkflowsRead, wf.Get},
		{"GET", "/orgs/{orgID}/workflows/{runID}/events", Tenant, rbac.WorkflowsRead, wf.Events},
		{"POST", "/orgs/{orgID}/workflows/{runID}/cancel", Tenant, rbac.WorkflowsCancel, wf.Cancel},
		{"POST", "/orgs/{orgID}/workflows/{runID}/retry", Tenant, rbac.WorkflowsRetry, wf.Retry},
		{"POST", "/orgs/{orgID}/workflows/{runID}/resume", Tenant, rbac.WorkflowsRun, wf.Resume},
		{"POST", "/orgs/{orgID}/workflows/{runID}/steps/{stepID}/complete", Tenant, rbac.WorkflowsRun, wf.Complete},
		{"GET", "/orgs/{orgID}/workflow-events", Tenant, rbac.WorkflowsRead, wf.Logs},
		{"GET", "/orgs/{orgID}/approvals", Tenant, rbac.WorkflowsRead, wf.ListApprovals},
		{"GET", "/orgs/{orgID}/approvals/{approvalID}", Tenant, rbac.WorkflowsRead, wf.GetApproval},
		{"POST", "/orgs/{orgID}/approvals/{approvalID}/decision", Tenant, rbac.ApprovalsDecide, wf.Decide},

		{"GET", "/orgs/{orgID}/products", Tenant, rbac.ProductsRead, pr.List},
		{"POST", "/orgs/{orgID}/products", Tenant, rbac.ProductsWrite, pr.Create},
		{"POST", "/orgs/{orgID}/products/publish", Tenant, rbac.WorkflowsRun, a.publish.Publish},
		{"GET", "/orgs/{orgID}/products/{productID}", Tenant, rbac.ProductsRead, pr.Get},
		{"PUT", "/orgs/{orgID}/products/{productID}", Tenant, rbac.ProductsWrite, pr.Replace},
		{"DELETE", "/orgs/{orgID}/products/{productID}", Tenant, rbac.ProductsWrite, pr.Delete},
		{"POST", "/orgs/{orgID}/products/{productID}/images", Tenant, rbac.ProductsWrite, pr.AddImage},
		{"DELETE", "/orgs/{orgID}/products/{productID}/images/{imageID}", Tenant, rbac.ProductsWrite, pr.DeleteImage},
		{"POST", "/orgs/{orgID}/imports", Tenant, rbac.ImportsWrite, pr.Upload},
		{"GET", "/orgs/{orgID}/imports", Tenant, rbac.ProductsRead, pr.ListBatches},
		{"GET", "/orgs/{orgID}/imports/sample.csv", Tenant, rbac.ProductsRead, pr.Sample},
		{"GET", "/orgs/{orgID}/imports/{batchID}", Tenant, rbac.ProductsRead, pr.GetBatch},
		{"GET", "/orgs/{orgID}/imports/{batchID}/rows", Tenant, rbac.ProductsRead, pr.Rows},
		{"PATCH", "/orgs/{orgID}/imports/{batchID}/rows/{rowID}", Tenant, rbac.ImportsWrite, pr.FixRow},
		{"POST", "/orgs/{orgID}/imports/{batchID}/retry", Tenant, rbac.ImportsWrite, pr.Retry},
		{"GET", "/orgs/{orgID}/imports/{batchID}/report", Tenant, rbac.ProductsRead, pr.Report},

		{"POST", "/orgs/{orgID}/extension/pairing-codes", Tenant, rbac.WorkflowsRun, bx.CreatePairingCode},
		{"GET", "/orgs/{orgID}/extension/devices", Tenant, rbac.WorkflowsRead, bx.ListDevices},
		{"DELETE", "/orgs/{orgID}/extension/devices/{deviceID}", Tenant, rbac.WorkflowsRun, bx.RevokeDevice},
		{"GET", "/orgs/{orgID}/workflows/{runID}/browser-tasks", Tenant, rbac.WorkflowsRead, bx.RunTasks},
		{"POST", "/orgs/{orgID}/browser-tasks/{taskID}/requeue", Tenant, rbac.WorkflowsRun, bx.Requeue},
		{"GET", "/orgs/{orgID}/browser-tasks/{taskID}/screenshots/{screenshotID}", Tenant, rbac.WorkflowsRead, bx.Screenshot},

		{"POST", "/ext/pair", ExtensionPublic, "", bx.Pair},
		{"GET", "/ext/me", Extension, "", bx.Me},
		{"POST", "/ext/token/rotate", Extension, "", bx.Rotate},
		{"DELETE", "/ext/device", Extension, "", bx.Unpair},
		{"GET", "/ext/tasks", Extension, "", bx.Tasks},
		{"POST", "/ext/tasks/{taskID}/claim", Extension, "", bx.Claim},
		{"POST", "/ext/tasks/{taskID}/heartbeat", Extension, "", bx.Heartbeat},
		{"POST", "/ext/tasks/{taskID}/screenshots", Extension, "", bx.AddScreenshot},
		{"POST", "/ext/tasks/{taskID}/result", Extension, "", bx.Result},
		{"POST", "/ext/tasks/{taskID}/release", Extension, "", bx.Release},

		{"GET", "/orgs/{orgID}/overview", Tenant, rbac.ShopsRead, a.overview.Org},
		{"GET", "/orgs/{orgID}/shops/{shopID}/activity", Tenant, rbac.ShopsRead, a.overview.Shop},
		{"GET", "/orgs/{orgID}/notifications", TenantSelf, "", a.notes.List},
		{"POST", "/orgs/{orgID}/notifications/read-all", TenantSelf, "", a.notes.ReadAll},
		{"POST", "/orgs/{orgID}/notifications/{notificationID}/read", TenantSelf, "", a.notes.Read},
		{"GET", "/orgs/{orgID}/shopee-apps", Tenant, rbac.ShopsConnect, sh.AppOptions},
		{"GET", "/orgs/{orgID}/shops/{shopID}/catalog/categories", Tenant, rbac.ProductsRead, a.catalog.Categories},
		{"GET", "/orgs/{orgID}/shops/{shopID}/catalog/categories/{categoryID}/attributes", Tenant, rbac.ProductsRead, a.catalog.Attributes},
		{"GET", "/orgs/{orgID}/shops/{shopID}/catalog/categories/{categoryID}/brands", Tenant, rbac.ProductsRead, a.catalog.Brands},
		{"GET", "/admin/capabilities", Admin, "", sh.Registry},
		{"PUT", "/admin/capability-overrides", Admin, "", sh.PutOverride},
		{"DELETE", "/admin/capability-overrides/{overrideID}", Admin, "", sh.DeleteOverride},

		{"GET", "/admin/browser-recipes", Admin, "", bx.ListRecipes},
		{"POST", "/admin/browser-recipes", Admin, "", bx.CreateRecipe},
		{"PATCH", "/admin/browser-recipes/{recipeID}", Admin, "", bx.UpdateRecipe},
		{"GET", "/admin/shopee-apps", Admin, "", sh.ListApps},
		{"POST", "/admin/shopee-apps", Admin, "", sh.CreateApp},
		{"PATCH", "/admin/shopee-apps/{appID}", Admin, "", sh.UpdateApp},
	}
}

// Handler returns the root HTTP handler.
func (a *App) Handler() http.Handler {
	cfg := a.deps.Config
	r := chi.NewRouter()
	r.Use(httpx.RequestMeta(cfg.TrustProxyHeaders), httpx.Recoverer(a.adapter), httpx.AccessLog(a.deps.Log), httpx.SecurityHeaders)
	r.NotFound(a.adapter.H(func(http.ResponseWriter, *http.Request) error { return apperr.NotFound("Route not found.") }).ServeHTTP)
	r.MethodNotAllowed(a.adapter.H(func(http.ResponseWriter, *http.Request) error {
		return &apperr.Error{Kind: apperr.KindNotFound, Code: "method_not_allowed", Message: "Method not allowed."}
	}).ServeHTTP)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Get("/readyz", a.ready)

	origin := cfg.PublicBaseURL.Scheme + "://" + cfg.PublicBaseURL.Host
	api := chi.NewRouter()
	api.Use(httpx.SameOrigin(a.adapter, origin), a.identity.LoadSession)
	api.NotFound(r.NotFoundHandler())
	api.MethodNotAllowed(r.MethodNotAllowedHandler())
	// Extension API: its own CORS and device-token auth, no session.
	ext := chi.NewRouter()
	ext.Use(a.browser.CORS)
	ext.NotFound(r.NotFoundHandler())
	ext.MethodNotAllowed(r.MethodNotAllowedHandler())
	for _, rt := range a.Routes() {
		if rt.Access == Webhook {
			// Outside the API sub-router: no origin check, no session.
			r.Method(rt.Method, "/api/v1"+rt.Path, a.adapter.H(rt.Handler))
			continue
		}
		if rt.Access == Extension || rt.Access == ExtensionPublic {
			h := a.adapter.H(rt.Handler)
			if rt.Access == Extension {
				h = a.browser.RequireDevice(h)
			}
			ext.Method(rt.Method, strings.TrimPrefix(rt.Path, "/ext"), h)
			continue
		}
		var mw []func(http.Handler) http.Handler
		switch rt.Access {
		case Admin:
			mw = append(mw, a.identity.RequireSession, a.requireAdmin)
		case Session:
			mw = append(mw, a.identity.RequireSession)
		case Tenant:
			mw = append(mw, a.identity.RequireSession, a.tenancy.ResolveTenant, tenancy.Require(a.deps.Log, rt.Perm))
		case TenantSelf:
			mw = append(mw, a.identity.RequireSession, a.tenancy.ResolveTenant)
		}
		api.With(mw...).Method(rt.Method, rt.Path, a.adapter.H(rt.Handler))
	}
	r.Mount("/api/v1/ext", ext)
	r.Mount("/api/v1", api)
	return otelhttp.NewHandler(r, "http.server", otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			return r.Method + " " + rc.RoutePattern()
		}
		return r.Method
	}))
}

// approvalPolicy implements Q-3: Operators may approve only runs started by
// someone else, unless the organization allows self-approval. Owners and
// admins may approve any run.
func approvalPolicy(t *tenancy.Service) workflow.ApprovalPolicy {
	return func(ctx context.Context, p authctx.Principal, tn authctx.Tenant, run workflowdb.WorkflowRun) error {
		if tn.Role != rbac.Operator || run.CreatedBy == nil || *run.CreatedBy != p.UserID {
			return nil
		}
		org, err := t.GetOrg(ctx, tn)
		if err != nil {
			return err
		}
		if !org.Settings.AllowSelfApproval {
			return apperr.Forbidden("self_approval_not_allowed", "Operators cannot approve their own runs in this organization.")
		}
		return nil
	}
}

// requireAdmin hides platform admin routes from everyone else.
func (a *App) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := authctx.PrincipalFrom(r.Context()); !ok || !p.IsPlatformAdmin {
			a.adapter.WriteError(w, r, apperr.NotFound("Route not found."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.deps.DB.Ping(ctx); err != nil {
		a.deps.Log.WarnContext(ctx, "readiness: database", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if a.deps.Redis != nil {
		if err := a.deps.Redis.Ping(ctx).Err(); err != nil {
			a.deps.Log.WarnContext(ctx, "readiness: redis", slog.String("error", err.Error()))
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
