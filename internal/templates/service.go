package templates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/capability"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/validate"
	"shopee-automation/internal/shops"
	"shopee-automation/internal/templates/templatesdb"
)

// Version statuses.
const (
	StatusDraft     = "DRAFT"
	StatusPublished = "PUBLISHED"
	StatusArchived  = "ARCHIVED"
)

const maxPreviewShops = 50

// ShopReader is the part of the shops service templates need.
type ShopReader interface {
	Get(ctx context.Context, t authctx.Tenant, id uuid.UUID) (shops.Detail, error)
}

// Service implements template use cases.
type Service struct {
	DB       *db.DB
	Clock    clock.Clock
	Audit    audit.Recorder
	Registry *Registry
	Shops    ShopReader
	Log      *slog.Logger
	// ActiveRuns counts workflow runs that use a template and are still
	// running (set by the app; nil means none).
	ActiveRuns func(ctx context.Context, orgID, templateID uuid.UUID) (int64, error)
}

func (s *Service) tx(ctx context.Context, p authctx.Principal, t authctx.Tenant, fn func(q *templatesdb.Queries, tx pgx.Tx) error) error {
	return s.DB.Tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(tx pgx.Tx) error { return fn(templatesdb.New(tx), tx) })
}

var (
	errTemplateNotFound = apperr.NotFound("Không tìm thấy mẫu shop.")
	errVersionNotFound  = apperr.NotFound("Không tìm thấy phiên bản của mẫu shop.")
	errNotDraft         = apperr.Conflict("version_not_draft", "Không thể sửa phiên bản đã phát hành. Hãy tạo bản nháp mới.")
)

// Template is the API view of a template.
type Template struct {
	ID                        uuid.UUID  `json:"id"`
	Name                      string     `json:"name"`
	Description               string     `json:"description"`
	CurrentPublishedVersionID *uuid.UUID `json:"current_published_version_id"`
	DraftVersionID            *uuid.UUID `json:"draft_version_id"`
	LatestVersionNo           int        `json:"latest_version_no"`
	ClonedFromVersionID       *uuid.UUID `json:"cloned_from_version_id"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

// Version is the API view of a template version.
type Version struct {
	ID            uuid.UUID  `json:"id"`
	TemplateID    uuid.UUID  `json:"template_id"`
	VersionNo     int        `json:"version_no"`
	Status        string     `json:"status"`
	BasedOnID     *uuid.UUID `json:"based_on_id"`
	ContentSHA256 *string    `json:"content_sha256"`
	PublishedAt   *time.Time `json:"published_at"`
	PublishedBy   *uuid.UUID `json:"published_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Section is one section of a version.
type Section struct {
	Key       string          `json:"key"`
	Content   json.RawMessage `json:"content"`
	ETag      string          `json:"etag"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func toTemplate(t templatesdb.ShopTemplate, draft *uuid.UUID, latest int) Template {
	return Template{ID: t.ID, Name: t.Name, Description: t.Description, CurrentPublishedVersionID: t.CurrentPublishedVersionID,
		DraftVersionID: draft, LatestVersionNo: latest, ClonedFromVersionID: t.ClonedFromVersionID,
		CreatedAt: t.CreatedAt.UTC(), UpdatedAt: t.UpdatedAt.UTC()}
}

func toVersion(v templatesdb.ShopTemplateVersion) Version {
	out := Version{ID: v.ID, TemplateID: v.TemplateID, VersionNo: int(v.VersionNo), Status: v.Status, BasedOnID: v.BasedOnID,
		PublishedBy: v.PublishedBy, CreatedAt: v.CreatedAt.UTC()}
	if v.ContentSha256 != nil {
		h := hex.EncodeToString(v.ContentSha256)
		out.ContentSHA256 = &h
	}
	if v.PublishedAt != nil {
		p := v.PublishedAt.UTC()
		out.PublishedAt = &p
	}
	return out
}

func sectionETag(t time.Time) string { return fmt.Sprintf(`W/"%d"`, t.UnixMicro()) }

func toSection(s templatesdb.ShopTemplateSection) Section {
	return Section{Key: s.SectionKey, Content: s.Content, ETag: sectionETag(s.UpdatedAt), UpdatedAt: s.UpdatedAt.UTC()}
}

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

// List returns the org's templates.
func (s *Service) List(ctx context.Context, p authctx.Principal, t authctx.Tenant) ([]Template, error) {
	out := []Template{}
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		rows, err := q.ListTemplates(ctx, t.OrgID)
		for _, r := range rows {
			out = append(out, toTemplate(r.ShopTemplate, r.DraftVersionID, int(r.LatestVersionNo)))
		}
		return err
	})
	return out, wrap(err, nil)
}

// CreateInput creates a template with an optional initial content.
type CreateInput struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Sections    map[string]json.RawMessage `json:"sections"`
}

// Created is the result of creating or cloning a template.
type Created struct {
	Template     Template `json:"template"`
	DraftVersion Version  `json:"draft_version"`
}

func (s *Service) checkMeta(name, description string) (string, string, error) {
	var v validate.Errors
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	v.Length("name", name, 1, 100)
	if len([]rune(description)) > 1000 {
		v.Add("description", "too_long", "Tối đa 1000 ký tự.")
	}
	return name, description, v.Err()
}

// checkSections validates the structure of a (possibly partial) section map.
func (s *Service) checkSections(secs map[string]json.RawMessage) error {
	var fields []apperr.FieldError
	for k, c := range secs {
		fields = append(fields, s.Registry.ValidateSection(k, c)...)
	}
	if len(fields) > 0 {
		return apperr.Invalid(fields...)
	}
	return nil
}

// Create makes a template and its first draft.
func (s *Service) Create(ctx context.Context, p authctx.Principal, t authctx.Tenant, in CreateInput) (Created, error) {
	name, desc, err := s.checkMeta(in.Name, in.Description)
	if err != nil {
		return Created{}, err
	}
	if err := s.checkSections(in.Sections); err != nil {
		return Created{}, err
	}
	content := Empty()
	for k, v := range in.Sections {
		content[k] = v
	}
	var out Created
	err = s.tx(ctx, p, t, func(q *templatesdb.Queries, tx pgx.Tx) error {
		var err error
		out, err = s.insertTemplate(ctx, q, p, t, name, desc, nil, content)
		if err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template.created",
			TargetType: "shop_template", TargetID: &out.Template.ID, Metadata: map[string]any{"name": name}})
	})
	return out, wrap(err, nil)
}

func (s *Service) insertTemplate(ctx context.Context, q *templatesdb.Queries, p authctx.Principal, t authctx.Tenant,
	name, desc string, clonedFrom *uuid.UUID, content Sections) (Created, error) {
	tpl, err := q.InsertTemplate(ctx, templatesdb.InsertTemplateParams{ID: db.NewID(), OrgID: t.OrgID, Name: name,
		Description: desc, ClonedFromVersionID: clonedFrom, CreatedBy: &p.UserID})
	if err != nil {
		if db.UniqueViolation(err) == "shop_templates_name_uk" {
			return Created{}, apperr.Invalid(apperr.FieldError{Field: "name", Code: "taken", Message: "Đã có mẫu shop với tên này."})
		}
		return Created{}, err
	}
	v, err := s.insertDraft(ctx, q, p, t, tpl.ID, clonedFrom, content)
	if err != nil {
		return Created{}, err
	}
	return Created{Template: toTemplate(tpl, &v.ID, int(v.VersionNo)), DraftVersion: toVersion(v)}, nil
}

func (s *Service) insertDraft(ctx context.Context, q *templatesdb.Queries, p authctx.Principal, t authctx.Tenant,
	templateID uuid.UUID, basedOn *uuid.UUID, content Sections) (templatesdb.ShopTemplateVersion, error) {
	v, err := q.InsertVersion(ctx, templatesdb.InsertVersionParams{ID: db.NewID(), OrgID: t.OrgID, TemplateID: templateID,
		BasedOnID: basedOn, CreatedBy: &p.UserID})
	if err != nil {
		if db.UniqueViolation(err) == "shop_template_versions_one_draft" {
			return v, apperr.Conflict("draft_exists", "Mẫu shop này đã có bản nháp. Hãy sửa hoặc phát hành bản nháp đó trước.")
		}
		return v, err
	}
	for _, key := range SectionKeys {
		c, ok := content[key]
		if !ok {
			c = json.RawMessage(`{}`)
		}
		if err := q.InsertSection(ctx, templatesdb.InsertSectionParams{ID: db.NewID(), OrgID: t.OrgID, VersionID: v.ID,
			SectionKey: key, Content: c, UpdatedBy: &p.UserID}); err != nil {
			return v, err
		}
	}
	return v, nil
}

// Detail is a template with its versions.
type Detail struct {
	Template Template  `json:"template"`
	Versions []Version `json:"versions"`
}

// Get returns a template with its versions, newest first.
func (s *Service) Get(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) (Detail, error) {
	var d Detail
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		tpl, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errTemplateNotFound)
		}
		vs, err := q.ListVersions(ctx, templatesdb.ListVersionsParams{OrgID: t.OrgID, TemplateID: id})
		if err != nil {
			return err
		}
		var draft *uuid.UUID
		d.Versions = make([]Version, 0, len(vs))
		for _, v := range vs {
			if v.Status == StatusDraft {
				draft = &v.ID
			}
			d.Versions = append(d.Versions, toVersion(v))
		}
		latest := 0
		if len(vs) > 0 {
			latest = int(vs[0].VersionNo)
		}
		d.Template = toTemplate(tpl, draft, latest)
		return nil
	})
	return d, wrap(err, nil)
}

// UpdateInput renames or re-describes a template.
type UpdateInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// Update changes name and description.
func (s *Service) Update(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID, in UpdateInput) (Template, error) {
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, tx pgx.Tx) error {
		cur, err := q.LockTemplate(ctx, templatesdb.LockTemplateParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errTemplateNotFound)
		}
		name, desc := cur.Name, cur.Description
		if in.Name != nil {
			name = *in.Name
		}
		if in.Description != nil {
			desc = *in.Description
		}
		if name, desc, err = s.checkMeta(name, desc); err != nil {
			return err
		}
		if _, err := q.UpdateTemplate(ctx, templatesdb.UpdateTemplateParams{Name: name, Description: desc, OrgID: t.OrgID, ID: id}); err != nil {
			if db.UniqueViolation(err) == "shop_templates_name_uk" {
				return apperr.Invalid(apperr.FieldError{Field: "name", Code: "taken", Message: "Đã có mẫu shop với tên này."})
			}
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template.updated",
			TargetType: "shop_template", TargetID: &id})
	})
	if err != nil {
		return Template{}, wrap(err, nil)
	}
	d, err := s.Get(ctx, p, t, id)
	return d.Template, err
}

// Delete soft-deletes a template. It is refused while workflow runs that
// use the template are still active.
func (s *Service) Delete(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) error {
	if s.ActiveRuns != nil {
		n, err := s.ActiveRuns(ctx, t.OrgID, id)
		if err != nil {
			return apperr.Internal(err)
		}
		if n > 0 {
			return apperr.Conflict("template_in_use", "Vẫn còn quy trình đang chạy với mẫu shop này. Hãy chờ chúng xong hoặc hủy trước.")
		}
	}
	return wrap(s.tx(ctx, p, t, func(q *templatesdb.Queries, tx pgx.Tx) error {
		if _, err := q.LockTemplate(ctx, templatesdb.LockTemplateParams{OrgID: t.OrgID, ID: id}); err != nil {
			return wrap(err, errTemplateNotFound)
		}
		if err := q.SoftDeleteTemplate(ctx, templatesdb.SoftDeleteTemplateParams{Now: s.Clock.Now(), OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template.deleted",
			TargetType: "shop_template", TargetID: &id})
	}), nil)
}

func (s *Service) loadSections(ctx context.Context, q *templatesdb.Queries, t authctx.Tenant, versionID uuid.UUID) (Sections, []Section, error) {
	rows, err := q.ListSections(ctx, templatesdb.ListSectionsParams{OrgID: t.OrgID, VersionID: versionID})
	if err != nil {
		return nil, nil, err
	}
	secs := Sections{}
	byKey := map[string]templatesdb.ShopTemplateSection{}
	for _, r := range rows {
		secs[r.SectionKey] = r.Content
		byKey[r.SectionKey] = r
	}
	list := make([]Section, 0, len(SectionKeys))
	for _, k := range SectionKeys {
		if r, ok := byKey[k]; ok {
			list = append(list, toSection(r))
		}
	}
	return secs, list, nil
}

// pickVersion returns the given version, or the current published one, or
// the latest version.
func (s *Service) pickVersion(ctx context.Context, q *templatesdb.Queries, t authctx.Tenant, tpl templatesdb.ShopTemplate, id *uuid.UUID) (templatesdb.ShopTemplateVersion, error) {
	if id == nil {
		id = tpl.CurrentPublishedVersionID
	}
	if id != nil {
		v, err := q.GetVersion(ctx, templatesdb.GetVersionParams{OrgID: t.OrgID, TemplateID: tpl.ID, ID: *id})
		return v, wrap(err, errVersionNotFound)
	}
	vs, err := q.ListVersions(ctx, templatesdb.ListVersionsParams{OrgID: t.OrgID, TemplateID: tpl.ID})
	if err != nil {
		return templatesdb.ShopTemplateVersion{}, err
	}
	if len(vs) == 0 {
		return templatesdb.ShopTemplateVersion{}, errVersionNotFound
	}
	return vs[0], nil
}

// CloneInput copies a template.
type CloneInput struct {
	Name          string     `json:"name"`
	FromVersionID *uuid.UUID `json:"from_version_id"`
}

// Clone creates a new template whose first draft copies a version
// (default: the published version, else the latest one).
func (s *Service) Clone(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID, in CloneInput) (Created, error) {
	name, _, err := s.checkMeta(in.Name, "")
	if err != nil {
		return Created{}, err
	}
	var out Created
	err = s.tx(ctx, p, t, func(q *templatesdb.Queries, tx pgx.Tx) error {
		src, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errTemplateNotFound)
		}
		v, err := s.pickVersion(ctx, q, t, src, in.FromVersionID)
		if err != nil {
			return err
		}
		secs, _, err := s.loadSections(ctx, q, t, v.ID)
		if err != nil {
			return err
		}
		if out, err = s.insertTemplate(ctx, q, p, t, name, src.Description, &v.ID, secs); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template.cloned",
			TargetType: "shop_template", TargetID: &out.Template.ID,
			Metadata: map[string]any{"source_template_id": id.String(), "source_version_id": v.ID.String()}})
	})
	return out, wrap(err, nil)
}

// VersionDetail is a version with its sections.
type VersionDetail struct {
	Version  Version   `json:"version"`
	Sections []Section `json:"sections"`
}

// GetVersion returns one version with its sections.
func (s *Service) GetVersion(ctx context.Context, p authctx.Principal, t authctx.Tenant, id, vid uuid.UUID) (VersionDetail, error) {
	var out VersionDetail
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		if _, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: id}); err != nil {
			return wrap(err, errTemplateNotFound)
		}
		v, err := q.GetVersion(ctx, templatesdb.GetVersionParams{OrgID: t.OrgID, TemplateID: id, ID: vid})
		if err != nil {
			return wrap(err, errVersionNotFound)
		}
		_, list, err := s.loadSections(ctx, q, t, v.ID)
		out = VersionDetail{Version: toVersion(v), Sections: list}
		return err
	})
	return out, wrap(err, nil)
}

// NewDraftInput starts a draft from an existing version.
type NewDraftInput struct {
	FromVersionID uuid.UUID `json:"from_version_id"`
}

// NewDraft copies a version into a new draft. Only one draft may be open.
func (s *Service) NewDraft(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID, in NewDraftInput) (Version, error) {
	var out Version
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, tx pgx.Tx) error {
		tpl, err := q.LockTemplate(ctx, templatesdb.LockTemplateParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errTemplateNotFound)
		}
		src, err := q.GetVersion(ctx, templatesdb.GetVersionParams{OrgID: t.OrgID, TemplateID: tpl.ID, ID: in.FromVersionID})
		if err != nil {
			return wrap(err, apperr.Invalid(apperr.FieldError{Field: "from_version_id", Code: "not_found", Message: "Không tìm thấy phiên bản này trong mẫu shop."}))
		}
		secs, _, err := s.loadSections(ctx, q, t, src.ID)
		if err != nil {
			return err
		}
		v, err := s.insertDraft(ctx, q, p, t, tpl.ID, &src.ID, secs)
		if err != nil {
			return err
		}
		out = toVersion(v)
		if err := q.TouchTemplate(ctx, templatesdb.TouchTemplateParams{OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template_version.created",
			TargetType: "shop_template_version", TargetID: &v.ID, Metadata: map[string]any{"template_id": id.String(), "version_no": v.VersionNo}})
	})
	return out, wrap(err, nil)
}

// PutSection replaces one section of a draft. ifMatch must be the section's
// current ETag.
func (s *Service) PutSection(ctx context.Context, p authctx.Principal, t authctx.Tenant, id, vid uuid.UUID, key string,
	content json.RawMessage, ifMatch string) (Section, error) {
	if _, ok := s.Registry.Section(key); !ok {
		return Section{}, apperr.NotFound("Mục không tồn tại.")
	}
	if fields := s.Registry.ValidateSection(key, content); fields != nil {
		return Section{}, apperr.Invalid(fields...)
	}
	if ifMatch == "" {
		return Section{}, apperr.PreconditionFailed("Thiếu header If-Match.")
	}
	var out Section
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, tx pgx.Tx) error {
		if _, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: id}); err != nil {
			return wrap(err, errTemplateNotFound)
		}
		v, err := q.LockVersion(ctx, templatesdb.LockVersionParams{OrgID: t.OrgID, TemplateID: id, ID: vid})
		if err != nil {
			return wrap(err, errVersionNotFound)
		}
		if v.Status != StatusDraft {
			return errNotDraft
		}
		cur, err := q.LockSection(ctx, templatesdb.LockSectionParams{OrgID: t.OrgID, VersionID: vid, SectionKey: key})
		if err != nil {
			return err
		}
		if sectionETag(cur.UpdatedAt) != ifMatch {
			return apperr.PreconditionFailed("Mục này vừa được người khác thay đổi. Hãy tải lại trang và thử lại.")
		}
		row, err := q.UpdateSection(ctx, templatesdb.UpdateSectionParams{Content: content, UpdatedBy: &p.UserID,
			OrgID: t.OrgID, VersionID: vid, SectionKey: key})
		if err != nil {
			return err
		}
		out = toSection(row)
		if err := q.TouchTemplate(ctx, templatesdb.TouchTemplateParams{OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template_section.updated",
			TargetType: "shop_template_version", TargetID: &vid, Metadata: map[string]any{"section": key}})
	})
	return out, wrap(err, nil)
}

// Validate reports errors and warnings of a version.
func (s *Service) Validate(ctx context.Context, p authctx.Principal, t authctx.Tenant, id, vid uuid.UUID) (Report, error) {
	var rep Report
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		if _, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: id}); err != nil {
			return wrap(err, errTemplateNotFound)
		}
		if _, err := q.GetVersion(ctx, templatesdb.GetVersionParams{OrgID: t.OrgID, TemplateID: id, ID: vid}); err != nil {
			return wrap(err, errVersionNotFound)
		}
		secs, _, err := s.loadSections(ctx, q, t, vid)
		rep = s.Registry.Validate(secs)
		return err
	})
	return rep, wrap(err, nil)
}

// Publish freezes a draft. The previous published version is archived.
func (s *Service) Publish(ctx context.Context, p authctx.Principal, t authctx.Tenant, id, vid uuid.UUID) (Version, error) {
	var out Version
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, tx pgx.Tx) error {
		tpl, err := q.LockTemplate(ctx, templatesdb.LockTemplateParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errTemplateNotFound)
		}
		v, err := q.LockVersion(ctx, templatesdb.LockVersionParams{OrgID: t.OrgID, TemplateID: id, ID: vid})
		if err != nil {
			return wrap(err, errVersionNotFound)
		}
		if v.Status != StatusDraft {
			return apperr.Conflict("version_not_draft", "Chỉ có thể phát hành bản nháp.")
		}
		secs, _, err := s.loadSections(ctx, q, t, vid)
		if err != nil {
			return err
		}
		if rep := s.Registry.Validate(secs); !rep.Valid {
			fields := make([]apperr.FieldError, 0, len(rep.Errors))
			for _, e := range rep.Errors {
				fields = append(fields, apperr.FieldError(e))
			}
			return apperr.Invalid(fields...)
		}
		canon, err := Canonical(secs)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(canon)
		if tpl.CurrentPublishedVersionID != nil {
			if err := q.ArchiveVersion(ctx, templatesdb.ArchiveVersionParams{OrgID: t.OrgID, ID: *tpl.CurrentPublishedVersionID}); err != nil {
				return err
			}
		}
		pv, err := q.PublishVersion(ctx, templatesdb.PublishVersionParams{ContentSha256: sum[:], Now: s.Clock.Now(),
			PublishedBy: &p.UserID, OrgID: t.OrgID, ID: vid})
		if err != nil {
			return err
		}
		if err := q.SetPublishedVersion(ctx, templatesdb.SetPublishedVersionParams{VersionID: &vid, OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		out = toVersion(pv)
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template_version.published",
			TargetType: "shop_template_version", TargetID: &vid,
			Metadata: map[string]any{"template_id": id.String(), "version_no": pv.VersionNo, "content_sha256": hex.EncodeToString(sum[:])}})
	})
	return out, wrap(err, nil)
}

// Overrides are partial sections for one shop.
type Overrides struct {
	ShopID    uuid.UUID                  `json:"shop_id"`
	Overrides map[string]json.RawMessage `json:"overrides"`
	UpdatedAt *time.Time                 `json:"updated_at"`
}

// OverridesInput replaces a shop's overrides.
type OverridesInput struct {
	Overrides map[string]json.RawMessage `json:"overrides"`
}

// GetOverrides returns the overrides of a shop (empty if none).
func (s *Service) GetOverrides(ctx context.Context, p authctx.Principal, t authctx.Tenant, id, shopID uuid.UUID) (Overrides, error) {
	out := Overrides{ShopID: shopID, Overrides: map[string]json.RawMessage{}}
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		if err := s.checkTemplateAndShop(ctx, q, t, id, shopID); err != nil {
			return err
		}
		m, err := q.GetMapping(ctx, templatesdb.GetMappingParams{OrgID: t.OrgID, TemplateID: id, ShopConnectionID: shopID})
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(m.Overrides, &out.Overrides); err != nil {
			return err
		}
		u := m.UpdatedAt.UTC()
		out.UpdatedAt = &u
		return nil
	})
	return out, wrap(err, nil)
}

func (s *Service) checkTemplateAndShop(ctx context.Context, q *templatesdb.Queries, t authctx.Tenant, id, shopID uuid.UUID) error {
	if _, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: id}); err != nil {
		return wrap(err, errTemplateNotFound)
	}
	ok, err := q.ShopExists(ctx, templatesdb.ShopExistsParams{OrgID: t.OrgID, ID: shopID})
	if err != nil {
		return err
	}
	if !ok {
		return apperr.NotFound("Không tìm thấy shop.")
	}
	return nil
}

// PutOverrides replaces a shop's overrides. They must form a valid template
// once merged on top of the template's published version (or its latest
// version when nothing is published).
func (s *Service) PutOverrides(ctx context.Context, p authctx.Principal, t authctx.Tenant, id, shopID uuid.UUID, in OverridesInput) (Overrides, error) {
	if in.Overrides == nil {
		in.Overrides = map[string]json.RawMessage{}
	}
	for k, v := range in.Overrides {
		if _, ok := s.Registry.Section(k); !ok {
			return Overrides{}, apperr.Invalid(apperr.FieldError{Field: "overrides." + k, Code: "unknown_section", Message: "Mục không tồn tại."})
		}
		var obj map[string]any
		if json.Unmarshal(v, &obj) != nil || obj == nil {
			return Overrides{}, apperr.Invalid(apperr.FieldError{Field: "overrides." + k, Code: "invalid", Message: "Phải là một đối tượng JSON."})
		}
	}
	var out Overrides
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, tx pgx.Tx) error {
		if err := s.checkTemplateAndShop(ctx, q, t, id, shopID); err != nil {
			return err
		}
		tpl, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return err
		}
		v, err := s.pickVersion(ctx, q, t, tpl, nil)
		if err != nil {
			return err
		}
		base, _, err := s.loadSections(ctx, q, t, v.ID)
		if err != nil {
			return err
		}
		merged, err := Merge(base, in.Overrides)
		if err != nil {
			return err
		}
		var fields []apperr.FieldError
		for k := range in.Overrides {
			for _, fe := range s.Registry.ValidateSection(k, merged[k]) {
				fe.Field = "overrides." + strings.TrimPrefix(fe.Field, "sections.")
				fields = append(fields, fe)
			}
		}
		if len(fields) > 0 {
			return apperr.Invalid(fields...)
		}
		raw, err := json.Marshal(in.Overrides)
		if err != nil {
			return err
		}
		m, err := q.UpsertMapping(ctx, templatesdb.UpsertMappingParams{ID: db.NewID(), OrgID: t.OrgID, TemplateID: id,
			ShopConnectionID: shopID, Overrides: raw, UpdatedBy: &p.UserID})
		if err != nil {
			return err
		}
		u := m.UpdatedAt.UTC()
		out = Overrides{ShopID: shopID, Overrides: in.Overrides, UpdatedAt: &u}
		keys := make([]string, 0, len(in.Overrides))
		for k := range in.Overrides {
			keys = append(keys, k)
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "template_overrides.updated",
			TargetType: "shop_template", TargetID: &id, Metadata: map[string]any{"shop_connection_id": shopID.String(), "sections": keys}})
	})
	return out, wrap(err, nil)
}

// PreviewInput lists the shops to preview.
type PreviewInput struct {
	ShopIDs []uuid.UUID `json:"shop_ids"`
}

// ShopPreview is the diff for one shop.
type ShopPreview struct {
	ShopID           uuid.UUID `json:"shop_id"`
	ShopName         *string   `json:"shop_name"`
	ConnectionStatus string    `json:"connection_status"`
	HasOverrides     bool      `json:"has_overrides"`
	Errors           []Issue   `json:"errors"`
	Warnings         []Issue   `json:"warnings"`
	Changes          []Change  `json:"changes"`
	Blocked          []Change  `json:"blocked"`
}

// Preview computes, per shop, the changes the version would make after
// merging the shop's overrides. Current values come from the last sync;
// fields Shopee was not read for are reported with current_known=false.
func (s *Service) Preview(ctx context.Context, p authctx.Principal, t authctx.Tenant, id, vid uuid.UUID, in PreviewInput) ([]ShopPreview, error) {
	if len(in.ShopIDs) == 0 || len(in.ShopIDs) > maxPreviewShops {
		return nil, apperr.Invalid(apperr.FieldError{Field: "shop_ids", Code: "invalid", Message: "Chọn từ 1 đến 50 shop."})
	}
	seen := map[uuid.UUID]bool{}
	for _, sid := range in.ShopIDs {
		if seen[sid] {
			return nil, apperr.Invalid(apperr.FieldError{Field: "shop_ids", Code: "duplicate", Message: "Mỗi shop chỉ được chọn một lần."})
		}
		seen[sid] = true
	}
	var base Sections
	overrides := map[uuid.UUID]Sections{}
	err := s.tx(ctx, p, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		if _, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: id}); err != nil {
			return wrap(err, errTemplateNotFound)
		}
		if _, err := q.GetVersion(ctx, templatesdb.GetVersionParams{OrgID: t.OrgID, TemplateID: id, ID: vid}); err != nil {
			return wrap(err, errVersionNotFound)
		}
		var err error
		if base, _, err = s.loadSections(ctx, q, t, vid); err != nil {
			return err
		}
		ms, err := q.ListMappings(ctx, templatesdb.ListMappingsParams{OrgID: t.OrgID, TemplateID: id})
		for _, m := range ms {
			var o Sections
			if json.Unmarshal(m.Overrides, &o) == nil && len(o) > 0 {
				overrides[m.ShopConnectionID] = o
			}
		}
		return err
	})
	if err != nil {
		return nil, wrap(err, nil)
	}

	out := make([]ShopPreview, 0, len(in.ShopIDs))
	for _, sid := range in.ShopIDs {
		detail, err := s.Shops.Get(ctx, t, sid)
		if err != nil {
			if apperr.IsKind(err, apperr.KindNotFound) {
				return nil, apperr.Invalid(apperr.FieldError{Field: "shop_ids", Code: "not_found", Message: "Không tìm thấy shop " + sid.String() + "."})
			}
			return nil, err
		}
		out = append(out, s.previewShop(base, overrides[sid], detail))
	}
	return out, nil
}

func (s *Service) previewShop(base, over Sections, d shops.Detail) ShopPreview {
	pv := ShopPreview{ShopID: d.Shop.ID, ShopName: d.Shop.ShopName, ConnectionStatus: d.Shop.Status, HasOverrides: len(over) > 0,
		Errors: []Issue{}, Warnings: []Issue{}, Changes: []Change{}, Blocked: []Change{}}
	target, err := Merge(base, over)
	if err != nil {
		pv.Errors = append(pv.Errors, Issue{Field: "overrides", Code: "invalid", Message: "Không đọc được giá trị riêng cho shop đã lưu."})
		return pv
	}
	rep := s.Registry.Validate(target)
	pv.Errors = append(pv.Errors, rep.Errors...)
	pv.Warnings = append(pv.Warnings, rep.Warnings...)
	if !rep.Valid {
		return pv
	}

	cur := Current{Values: map[string]any{}, Known: map[string]bool{}}
	if d.Shop.Region != nil {
		cur.Region = *d.Shop.Region
	}
	if d.Profile != nil {
		if d.Profile.ShopName != nil {
			cur.Values["general.shop_name"], cur.Known["general.shop_name"] = *d.Profile.ShopName, true
		}
		cur.Values["description.text"], cur.Known["description.text"] = derefStr(d.Profile.Description), true
		if d.Profile.LogoURL != nil {
			cur.Values["branding.logo_url"], cur.Known["branding.logo_url"] = *d.Profile.LogoURL, true
		}
	} else {
		pv.Warnings = append(pv.Warnings, Issue{Field: "shop", Code: "not_synced", Message: "Shop chưa được đồng bộ nên chưa biết giá trị hiện tại."})
	}
	var g general
	_ = json.Unmarshal(target["general"], &g)
	if g.Region != "" && cur.Region != "" && g.Region != cur.Region {
		pv.Warnings = append(pv.Warnings, Issue{Field: "sections.general.region", Code: "region_mismatch",
			Message: "Mẫu shop dành cho thị trường " + g.Region + " nhưng shop này thuộc " + cur.Region + "."})
	}

	caps := map[string]capability.Resolved{}
	for _, c := range d.Capabilities {
		caps[c.Key] = c
	}
	for _, ch := range s.Registry.Diff(target, cur, caps) {
		if ch.Enabled {
			pv.Changes = append(pv.Changes, ch)
		} else {
			pv.Blocked = append(pv.Blocked, ch)
		}
	}
	return pv
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Resolved is a published version merged with one shop's overrides.
type Resolved struct {
	TemplateID   uuid.UUID `json:"template_id"`
	TemplateName string    `json:"template_name"`
	VersionID    uuid.UUID `json:"version_id"`
	VersionNo    int       `json:"version_no"`
	Status       string    `json:"status"`
	Sections     Sections  `json:"sections"`
	HasOverrides bool      `json:"has_overrides"`
}

// Resolve returns a version merged with the shop's current overrides.
func (s *Service) Resolve(ctx context.Context, t authctx.Tenant, templateID, versionID, shopID uuid.UUID) (Resolved, error) {
	var out Resolved
	err := s.tx(ctx, authctx.Principal{}, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		tpl, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: templateID})
		if err != nil {
			return wrap(err, errTemplateNotFound)
		}
		v, err := q.GetVersion(ctx, templatesdb.GetVersionParams{OrgID: t.OrgID, TemplateID: templateID, ID: versionID})
		if err != nil {
			return wrap(err, errVersionNotFound)
		}
		base, _, err := s.loadSections(ctx, q, t, v.ID)
		if err != nil {
			return err
		}
		var over Sections
		m, err := q.GetMapping(ctx, templatesdb.GetMappingParams{OrgID: t.OrgID, TemplateID: templateID, ShopConnectionID: shopID})
		switch {
		case err == nil:
			if err := json.Unmarshal(m.Overrides, &over); err != nil {
				return err
			}
		case !db.IsNoRows(err):
			return err
		}
		merged, err := Merge(base, over)
		if err != nil {
			return err
		}
		out = Resolved{TemplateID: tpl.ID, TemplateName: tpl.Name, VersionID: v.ID, VersionNo: int(v.VersionNo), Status: v.Status,
			Sections: merged, HasOverrides: len(over) > 0}
		return nil
	})
	return out, wrap(err, nil)
}

// VersionStatus returns a version's status and number.
func (s *Service) VersionStatus(ctx context.Context, t authctx.Tenant, templateID, versionID uuid.UUID) (string, int, string, error) {
	var status, name string
	var no int
	err := s.tx(ctx, authctx.Principal{}, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		tpl, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: templateID})
		if err != nil {
			return wrap(err, errTemplateNotFound)
		}
		v, err := q.GetVersion(ctx, templatesdb.GetVersionParams{OrgID: t.OrgID, TemplateID: templateID, ID: versionID})
		if err != nil {
			return wrap(err, errVersionNotFound)
		}
		status, no, name = v.Status, int(v.VersionNo), tpl.Name
		return nil
	})
	return status, no, name, wrap(err, nil)
}

// VersionSections returns the sections of a version (used by imports for
// defaults such as category mappings and SKU rules).
func (s *Service) VersionSections(ctx context.Context, t authctx.Tenant, templateID, versionID uuid.UUID) (map[string]json.RawMessage, error) {
	var out Sections
	err := s.tx(ctx, authctx.Principal{}, t, func(q *templatesdb.Queries, _ pgx.Tx) error {
		if _, err := q.GetTemplate(ctx, templatesdb.GetTemplateParams{OrgID: t.OrgID, ID: templateID}); err != nil {
			return wrap(err, errTemplateNotFound)
		}
		if _, err := q.GetVersion(ctx, templatesdb.GetVersionParams{OrgID: t.OrgID, TemplateID: templateID, ID: versionID}); err != nil {
			return wrap(err, errVersionNotFound)
		}
		var err error
		out, _, err = s.loadSections(ctx, q, t, versionID)
		return err
	})
	return out, wrap(err, nil)
}
