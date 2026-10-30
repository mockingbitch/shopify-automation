package products

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/httpx"
	"shopee-automation/internal/platform/media"
)

// Handler serves product and import endpoints.
type Handler struct {
	Svc *Service
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
		return nil, apperr.Invalid(apperr.FieldError{Field: key, Code: "invalid", Message: "Must be a UUID."})
	}
	return &id, nil
}

// readFile reads the multipart field "file" with a size limit.
func readFile(w http.ResponseWriter, r *http.Request, limit int64) (string, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // G120: body is bounded by MaxBytesReader above
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return "", nil, &apperr.Error{Kind: apperr.KindPayloadTooLarge, Code: "payload_too_large", Message: "The file is too large."}
		}
		return "", nil, &apperr.Error{Kind: apperr.KindUnsupportedMediaType, Code: "unsupported_media_type", Message: "Send multipart/form-data with a file field."}
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		return "", nil, apperr.Invalid(apperr.FieldError{Field: "file", Code: "required", Message: "Choose a file."})
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", nil, apperr.BadRequest("bad_request", "Could not read the file.")
	}
	if int64(len(b)) > limit {
		return "", nil, &apperr.Error{Kind: apperr.KindPayloadTooLarge, Code: "payload_too_large", Message: "The file is too large."}
	}
	return hdr.Filename, b, nil
}

// List handles GET /orgs/{orgID}/products.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	limit, err := httpx.QueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	batch, err := optUUID(r, "batch_id")
	if err != nil {
		return err
	}
	var q *string
	if v := r.URL.Query().Get("q"); v != "" {
		if len(v) > 100 {
			return apperr.Invalid(apperr.FieldError{Field: "q", Code: "too_long", Message: "Must be at most 100 characters."})
		}
		q = &v
	}
	p, t := parts(r)
	out, next, err := h.Svc.List(r.Context(), p, t, ListFilter{Q: q, BatchID: batch, Limit: limit, Cursor: r.URL.Query().Get("cursor")})
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[Product]{Data: out, NextCursor: next})
}

// Create handles POST /orgs/{orgID}/products.
func (h Handler) Create(w http.ResponseWriter, r *http.Request) error {
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Svc.Create(r.Context(), p, t, in)
	if err != nil {
		return err
	}
	w.Header().Set("ETag", out.ETag())
	return httpx.JSON(w, http.StatusCreated, httpx.Data[Product]{Data: out})
}

// Get handles GET /orgs/{orgID}/products/{productID}.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "productID", errNotFound)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Svc.Get(r.Context(), p, t, id)
	if err != nil {
		return err
	}
	w.Header().Set("ETag", out.ETag())
	return httpx.JSON(w, http.StatusOK, httpx.Data[Product]{Data: out})
}

// Replace handles PUT /orgs/{orgID}/products/{productID}.
func (h Handler) Replace(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "productID", errNotFound)
	if err != nil {
		return err
	}
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Svc.Replace(r.Context(), p, t, id, in, r.Header.Get("If-Match"))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", out.ETag())
	return httpx.JSON(w, http.StatusOK, httpx.Data[Product]{Data: out})
}

// Delete handles DELETE /orgs/{orgID}/products/{productID}.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "productID", errNotFound)
	if err != nil {
		return err
	}
	p, t := parts(r)
	if err := h.Svc.Delete(r.Context(), p, t, id); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// AddImage handles POST /orgs/{orgID}/products/{productID}/images.
func (h Handler) AddImage(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "productID", errNotFound)
	if err != nil {
		return err
	}
	_, data, err := readFile(w, r, media.MaxBytes)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Svc.AddImage(r.Context(), p, t, id, data)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusCreated, httpx.Data[Image]{Data: out})
}

// DeleteImage handles DELETE /orgs/{orgID}/products/{productID}/images/{imageID}.
func (h Handler) DeleteImage(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "productID", errNotFound)
	if err != nil {
		return err
	}
	imageID, err := pathID(r, "imageID", apperr.NotFound("Image not found."))
	if err != nil {
		return err
	}
	p, t := parts(r)
	if err := h.Svc.DeleteImage(r.Context(), p, t, id, imageID); err != nil {
		return err
	}
	return httpx.NoContent(w)
}

// Upload handles POST /orgs/{orgID}/imports.
func (h Handler) Upload(w http.ResponseWriter, r *http.Request) error {
	name, data, err := readFile(w, r, MaxImportBytes)
	if err != nil {
		return err
	}
	in := UploadInput{Filename: name, Data: data}
	for field, dst := range map[string]**uuid.UUID{"template_id": &in.TemplateID, "template_version_id": &in.TemplateVersionID} {
		if v := r.FormValue(field); v != "" {
			id, err := uuid.Parse(v)
			if err != nil {
				return apperr.Invalid(apperr.FieldError{Field: field, Code: "invalid", Message: "Must be a UUID."})
			}
			*dst = &id
		}
	}
	p, t := parts(r)
	out, err := h.Svc.Upload(r.Context(), p, t, in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[Batch]{Data: out})
}

// ListBatches handles GET /orgs/{orgID}/imports.
func (h Handler) ListBatches(w http.ResponseWriter, r *http.Request) error {
	p, t := parts(r)
	out, err := h.Svc.ListBatches(r.Context(), p, t)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[Batch]{Data: out})
}

// GetBatch handles GET /orgs/{orgID}/imports/{batchID}.
func (h Handler) GetBatch(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "batchID", errBatchNotFound)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Svc.GetBatch(r.Context(), p, t, id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Batch]{Data: out})
}

// Rows handles GET /orgs/{orgID}/imports/{batchID}/rows.
func (h Handler) Rows(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "batchID", errBatchNotFound)
	if err != nil {
		return err
	}
	limit, err := httpx.QueryInt(r, "limit", 100, 1, 500)
	if err != nil {
		return err
	}
	after := 0
	if c := r.URL.Query().Get("cursor"); c != "" {
		if after, err = strconv.Atoi(c); err != nil || after < 0 {
			return apperr.Invalid(apperr.FieldError{Field: "cursor", Code: "invalid", Message: "Invalid cursor."})
		}
	}
	var status *string
	if v := r.URL.Query().Get("status"); v != "" {
		status = &v
	}
	p, t := parts(r)
	out, next, err := h.Svc.ListRows(r.Context(), p, t, id, status, after, limit)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.List[Row]{Data: out, NextCursor: next})
}

// FixRow handles PATCH /orgs/{orgID}/imports/{batchID}/rows/{rowID}.
func (h Handler) FixRow(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "batchID", errBatchNotFound)
	if err != nil {
		return err
	}
	rowID, err := pathID(r, "rowID", apperr.NotFound("Row not found."))
	if err != nil {
		return err
	}
	var in FixInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Svc.FixRow(r.Context(), p, t, id, rowID, in)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[Row]{Data: out})
}

// Retry handles POST /orgs/{orgID}/imports/{batchID}/retry.
func (h Handler) Retry(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "batchID", errBatchNotFound)
	if err != nil {
		return err
	}
	p, t := parts(r)
	out, err := h.Svc.Retry(r.Context(), p, t, id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[Batch]{Data: out})
}

// Report handles GET /orgs/{orgID}/imports/{batchID}/report.
func (h Handler) Report(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "batchID", errBatchNotFound)
	if err != nil {
		return err
	}
	p, t := parts(r)
	u, exp, err := h.Svc.ReportURL(r.Context(), p, t, id)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusOK, httpx.Data[map[string]any]{Data: map[string]any{"url": u, "expires_at": exp.Format(time.RFC3339)}})
}

// Sample handles GET /orgs/{orgID}/imports/sample.csv.
func (h Handler) Sample(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="products-sample.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	_, err := io.WriteString(w, SampleCSV)
	return err
}
