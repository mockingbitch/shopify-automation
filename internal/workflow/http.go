package workflow

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/httpx"
)

// Handler serves workflow, approval and execution-log endpoints.
type Handler struct {
	Engine *Engine
	Policy ApprovalPolicy
}

func parts(r *http.Request) (authctx.Principal, authctx.Tenant) {
	return authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context())
}

func pathID(r *http.Request, name string, notFound error) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, notFound
	}
	return id, nil
}

func optUUID(r *http.Request, key string) (*uuid.UUID, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, apperr.Invalid(apperr.FieldError{Field: key, Code: "invalid", Message: "Phải là UUID hợp lệ."})
	}
	return &id, nil
}

func optStr(r *http.Request, key string) *string {
	if v := r.URL.Query().Get(key); v != "" {
		return &v
	}
	return nil
}

// List handles GET /orgs/{orgID}/workflows.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	limit, err := httpx.QueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	shop, err := optUUID(r, "shop_id")
	if err != nil {
		return err
	}
	batch, err := optUUID(r, "batch_id")
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, next, err := h.Engine.ListRuns(r.Context(), p, t, ListFilter{Status: optStr(r, "status"), ShopID: shop, BatchID: batch,
		Limit: limit, Cursor: r.URL.Query().Get("cursor")})
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[RunSummary]{Data: out, NextCursor: next})
}

// Get handles GET /orgs/{orgID}/workflows/{runID}.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "runID", errRunNotFound)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Engine.GetRun(r.Context(), p, t, id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[RunView]{Data: out})
}

// Events handles GET /orgs/{orgID}/workflows/{runID}/events.
func (h Handler) Events(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "runID", errRunNotFound)
	if err != nil {
		return err
	}
	limit, err := httpx.QueryInt(r, "limit", 200, 1, 500)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, next, err := h.Engine.Events(r.Context(), p, t, id, r.URL.Query().Get("after"), limit)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[EventView]{Data: out, NextCursor: next})
}

// Logs handles GET /orgs/{orgID}/workflow-events (execution log).
func (h Handler) Logs(w http.ResponseWriter, r *http.Request) error {
	limit, err := httpx.QueryInt(r, "limit", 100, 1, 200)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, next, err := h.Engine.OrgEvents(r.Context(), p, t, optStr(r, "level"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[EventView]{Data: out, NextCursor: next})
}

func (h Handler) runAction(w http.ResponseWriter, r *http.Request, fn func(p authctx.Principal, t authctx.Tenant, id uuid.UUID) (RunView, error)) error {
	id, err := pathID(r, "runID", errRunNotFound)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, err := fn(p, t, id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[RunView]{Data: out})
}

// Cancel handles POST /orgs/{orgID}/workflows/{runID}/cancel.
func (h Handler) Cancel(w http.ResponseWriter, r *http.Request) error {
	var in CancelInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	return h.runAction(w, r, func(p authctx.Principal, t authctx.Tenant, id uuid.UUID) (RunView, error) {
		return h.Engine.Cancel(r.Context(), p, t, id, in)
	})
}

// Retry handles POST /orgs/{orgID}/workflows/{runID}/retry.
func (h Handler) Retry(w http.ResponseWriter, r *http.Request) error {
	return h.runAction(w, r, func(p authctx.Principal, t authctx.Tenant, id uuid.UUID) (RunView, error) {
		return h.Engine.Retry(r.Context(), p, t, id)
	})
}

// Resume handles POST /orgs/{orgID}/workflows/{runID}/resume.
func (h Handler) Resume(w http.ResponseWriter, r *http.Request) error {
	return h.runAction(w, r, func(p authctx.Principal, t authctx.Tenant, id uuid.UUID) (RunView, error) {
		return h.Engine.Resume(r.Context(), p, t, id)
	})
}

// Complete handles POST /orgs/{orgID}/workflows/{runID}/steps/{stepID}/complete.
func (h Handler) Complete(w http.ResponseWriter, r *http.Request) error {
	stepID, err := pathID(r, "stepID", errStepNotFound)
	if err != nil {
		return err
	}
	var in CompleteInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	return h.runAction(w, r, func(p authctx.Principal, t authctx.Tenant, id uuid.UUID) (RunView, error) {
		return h.Engine.CompleteManual(r.Context(), p, t, id, stepID, in)
	})
}

// ListApprovals handles GET /orgs/{orgID}/approvals.
func (h Handler) ListApprovals(w http.ResponseWriter, r *http.Request) error {
	p, t := parts(r)
	out, err := h.Engine.ListApprovals(r.Context(), p, t, optStr(r, "status"))
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[ApprovalView]{Data: out})
}

// GetApproval handles GET /orgs/{orgID}/approvals/{approvalID}.
func (h Handler) GetApproval(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "approvalID", errApprovalNotFound)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Engine.GetApproval(r.Context(), p, t, id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[ApprovalView]{Data: out})
}

// Decide handles POST /orgs/{orgID}/approvals/{approvalID}/decision.
func (h Handler) Decide(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "approvalID", errApprovalNotFound)
	if err != nil {
		return err
	}
	var in DecisionInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Engine.Decide(r.Context(), p, t, id, in, h.Policy)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[ApprovalView]{Data: out})
}
