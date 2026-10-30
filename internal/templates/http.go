package templates

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/httpx"
)

// Handler serves template endpoints.
type Handler struct {
	Svc *Service
}

func ids(r *http.Request, names ...string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(names))
	for _, n := range names {
		id, err := uuid.Parse(chi.URLParam(r, n))
		if err != nil {
			if n == "templateID" {
				return nil, errTemplateNotFound
			}
			if n == "shopID" {
				return nil, apperr.NotFound("Shop not found.")
			}
			return nil, errVersionNotFound
		}
		out = append(out, id)
	}
	return out, nil
}

func ctxParts(r *http.Request) (authctx.Principal, authctx.Tenant) {
	return authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context())
}

// SectionSchemaView is one section schema as served to clients.
type SectionSchemaView struct {
	Key       string          `json:"key"`
	AppliesTo string          `json:"applies_to"`
	Schema    json.RawMessage `json:"schema"`
}

// Schemas handles GET /template-sections.
func (h Handler) Schemas(w http.ResponseWriter, _ *http.Request) error {
	out := make([]SectionSchemaView, 0, len(SectionKeys))
	for _, k := range SectionKeys {
		s, _ := h.Svc.Registry.Section(k)
		out = append(out, SectionSchemaView{Key: k, AppliesTo: s.AppliesTo, Schema: s.Raw})
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[SectionSchemaView]{Data: out})
}

// List handles GET /orgs/{orgID}/templates.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	p, t := ctxParts(r)
	out, err := h.Svc.List(r.Context(), p, t)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[Template]{Data: out})
}

// Create handles POST /orgs/{orgID}/templates.
func (h Handler) Create(w http.ResponseWriter, r *http.Request) error {
	var in CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.Create(r.Context(), p, t, in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.Data[Created]{Data: out})
}

// Get handles GET /orgs/{orgID}/templates/{templateID}.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := ids(r, "templateID")
	if err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.Get(r.Context(), p, t, id[0])
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Detail]{Data: out})
}

// Update handles PATCH /orgs/{orgID}/templates/{templateID}.
func (h Handler) Update(w http.ResponseWriter, r *http.Request) error {
	id, err := ids(r, "templateID")
	if err != nil {
		return err
	}
	var in UpdateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.Update(r.Context(), p, t, id[0], in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Template]{Data: out})
}

// Delete handles DELETE /orgs/{orgID}/templates/{templateID}.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	id, err := ids(r, "templateID")
	if err != nil {
		return err
	}
	p, t := ctxParts(r)
	if err := h.Svc.Delete(r.Context(), p, t, id[0]); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// Clone handles POST /orgs/{orgID}/templates/{templateID}/clone.
func (h Handler) Clone(w http.ResponseWriter, r *http.Request) error {
	id, err := ids(r, "templateID")
	if err != nil {
		return err
	}
	var in CloneInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.Clone(r.Context(), p, t, id[0], in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.Data[Created]{Data: out})
}

// GetVersion handles GET /orgs/{orgID}/templates/{templateID}/versions/{versionID}.
func (h Handler) GetVersion(w http.ResponseWriter, r *http.Request) error {
	v, err := ids(r, "templateID", "versionID")
	if err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.GetVersion(r.Context(), p, t, v[0], v[1])
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[VersionDetail]{Data: out})
}

// NewDraft handles POST /orgs/{orgID}/templates/{templateID}/versions.
func (h Handler) NewDraft(w http.ResponseWriter, r *http.Request) error {
	id, err := ids(r, "templateID")
	if err != nil {
		return err
	}
	var in NewDraftInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.NewDraft(r.Context(), p, t, id[0], in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.Data[Version]{Data: out})
}

// PutSection handles PUT .../versions/{versionID}/sections/{sectionKey}.
func (h Handler) PutSection(w http.ResponseWriter, r *http.Request) error {
	v, err := ids(r, "templateID", "versionID")
	if err != nil {
		return err
	}
	var content json.RawMessage
	if err := httpx.Decode(w, r, &content); err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.PutSection(r.Context(), p, t, v[0], v[1], chi.URLParam(r, "sectionKey"), content, r.Header.Get("If-Match"))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", out.ETag)
	return httpx.JSON(w, http.StatusOK, httpx.Data[Section]{Data: out})
}

// Validate handles POST .../versions/{versionID}/validate.
func (h Handler) Validate(w http.ResponseWriter, r *http.Request) error {
	v, err := ids(r, "templateID", "versionID")
	if err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.Validate(r.Context(), p, t, v[0], v[1])
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Report]{Data: out})
}

// Publish handles POST .../versions/{versionID}/publish.
func (h Handler) Publish(w http.ResponseWriter, r *http.Request) error {
	v, err := ids(r, "templateID", "versionID")
	if err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.Publish(r.Context(), p, t, v[0], v[1])
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Version]{Data: out})
}

// Preview handles POST .../versions/{versionID}/preview.
func (h Handler) Preview(w http.ResponseWriter, r *http.Request) error {
	v, err := ids(r, "templateID", "versionID")
	if err != nil {
		return err
	}
	var in PreviewInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.Preview(r.Context(), p, t, v[0], v[1], in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[ShopPreview]{Data: out})
}

// GetOverrides handles GET .../templates/{templateID}/shops/{shopID}/overrides.
func (h Handler) GetOverrides(w http.ResponseWriter, r *http.Request) error {
	v, err := ids(r, "templateID", "shopID")
	if err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.GetOverrides(r.Context(), p, t, v[0], v[1])
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Overrides]{Data: out})
}

// PutOverrides handles PUT .../templates/{templateID}/shops/{shopID}/overrides.
func (h Handler) PutOverrides(w http.ResponseWriter, r *http.Request) error {
	v, err := ids(r, "templateID", "shopID")
	if err != nil {
		return err
	}
	var in OverridesInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := ctxParts(r)
	out, err := h.Svc.PutOverrides(r.Context(), p, t, v[0], v[1], in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Overrides]{Data: out})
}
