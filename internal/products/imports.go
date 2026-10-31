package products

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/products/productsdb"
)

// TemplateDefaults gives an import the sections of a template version.
type TemplateDefaults interface {
	VersionSections(ctx context.Context, t authctx.Tenant, templateID, versionID uuid.UUID) (map[string]json.RawMessage, error)
}

// Batch statuses.
const (
	BatchUploaded  = "UPLOADED"
	BatchParsing   = "PARSING"
	BatchImporting = "IMPORTING"
	BatchCompleted = "COMPLETED"
	BatchFailed    = "FAILED"
)

var errBatchNotFound = apperr.NotFound("Không tìm thấy lô nhập.")

// Batch is the API view of an import.
type Batch struct {
	ID                uuid.UUID  `json:"id"`
	Source            string     `json:"source"`
	Filename          string     `json:"filename"`
	Status            string     `json:"status"`
	TemplateID        *uuid.UUID `json:"template_id"`
	TemplateVersionID *uuid.UUID `json:"template_version_id"`
	TotalRows         int        `json:"total_rows"`
	ValidRows         int        `json:"valid_rows"`
	InvalidRows       int        `json:"invalid_rows"`
	ImportedRows      int        `json:"imported_rows"`
	FailedRows        int        `json:"failed_rows"`
	HasReport         bool       `json:"has_report"`
	Error             *string    `json:"error"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func toBatch(b productsdb.ProductImportBatch) Batch {
	return Batch{ID: b.ID, Source: b.Source, Filename: b.Filename, Status: b.Status, TemplateID: b.TemplateID,
		TemplateVersionID: b.TemplateVersionID, TotalRows: int(b.TotalRows), ValidRows: int(b.ValidRows), InvalidRows: int(b.InvalidRows),
		ImportedRows: int(b.ImportedRows), FailedRows: int(b.FailedRows), HasReport: b.ReportStorageKey != nil, Error: b.Error,
		CreatedAt: b.CreatedAt.UTC(), UpdatedAt: b.UpdatedAt.UTC()}
}

// Row is the API view of an import row.
type Row struct {
	ID        uuid.UUID         `json:"id"`
	RowNumber int               `json:"row_number"`
	Values    map[string]string `json:"values"`
	Status    string            `json:"status"`
	Errors    []RowError        `json:"errors"`
	ProductID *uuid.UUID        `json:"product_id"`
}

func toRow(r productsdb.ProductImportRow) Row {
	out := Row{ID: r.ID, RowNumber: int(r.RowNumber), Status: r.Status, ProductID: r.ProductID, Values: map[string]string{}, Errors: []RowError{}}
	_ = json.Unmarshal(r.Raw, &out.Values)
	_ = json.Unmarshal(r.Errors, &out.Errors)
	return out
}

// UploadInput is an import file plus optional template defaults.
type UploadInput struct {
	Filename          string
	Data              []byte
	TemplateID        *uuid.UUID
	TemplateVersionID *uuid.UUID
}

// ImportPayload is the task payload of queue.TypeImport.
type ImportPayload struct {
	OrgID   uuid.UUID `json:"org_id"`
	BatchID uuid.UUID `json:"batch_id"`
	UserID  uuid.UUID `json:"user_id"`
	Parse   bool      `json:"parse"`
}

// Upload stores the file and queues parsing.
func (s *Service) Upload(ctx context.Context, p authctx.Principal, t authctx.Tenant, in UploadInput) (Batch, error) {
	ext := strings.ToLower(filepath.Ext(in.Filename))
	kind := map[string]string{".csv": "CSV", ".xlsx": "XLSX"}[ext]
	if kind == "" {
		return Batch{}, apperr.Invalid(apperr.FieldError{Field: "file", Code: "unsupported", Message: "Hãy tải lên tệp .csv hoặc .xlsx."})
	}
	if len(in.Data) == 0 {
		return Batch{}, apperr.Invalid(apperr.FieldError{Field: "file", Code: "empty", Message: "Tệp không có nội dung."})
	}
	if err := s.scanFile(ctx, "file", in.Data); err != nil {
		return Batch{}, err
	}
	if (in.TemplateID == nil) != (in.TemplateVersionID == nil) {
		return Batch{}, apperr.Invalid(apperr.FieldError{Field: "template_version_id", Code: "required", Message: "Hãy chọn cả mẫu shop và phiên bản (template_id và template_version_id)."})
	}
	if in.TemplateID != nil && s.Templates != nil {
		if _, err := s.Templates.VersionSections(ctx, t, *in.TemplateID, *in.TemplateVersionID); err != nil {
			return Batch{}, apperr.Invalid(apperr.FieldError{Field: "template_version_id", Code: "not_found", Message: "Không tìm thấy phiên bản mẫu shop."})
		}
	}
	id := db.NewID()
	key := "orgs/" + t.OrgID.String() + "/imports/" + id.String() + "/source" + ext
	ct := "text/csv"
	if kind == "XLSX" {
		ct = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	if err := s.Store.Put(ctx, key, in.Data, ct); err != nil {
		return Batch{}, apperr.Unavailable("storage_unavailable", "Không lưu được tệp. Hãy thử lại.")
	}
	sum := sha256.Sum256(in.Data)
	name := filepath.Base(in.Filename)
	if len(name) > 200 {
		name = name[:200]
	}
	var out Batch
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		b, err := q.InsertBatch(ctx, productsdb.InsertBatchParams{ID: id, OrgID: t.OrgID, Source: kind, Filename: name, FileStorageKey: key,
			FileSha256: sum[:], TemplateID: in.TemplateID, TemplateVersionID: in.TemplateVersionID, CreatedBy: &p.UserID})
		if err != nil {
			return err
		}
		out = toBatch(b)
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "import.uploaded",
			TargetType: "import_batch", TargetID: &id, Metadata: map[string]any{"filename": name, "bytes": len(in.Data)}})
	})
	if err != nil {
		return Batch{}, wrap(err, nil)
	}
	s.enqueueImport(ctx, ImportPayload{OrgID: t.OrgID, BatchID: id, UserID: p.UserID, Parse: true})
	return out, nil
}

func (s *Service) enqueueImport(ctx context.Context, p ImportPayload) {
	if _, err := s.Queue.Enqueue(ctx, queue.TypeImport, p, queue.Options{MaxRetry: 3, Timeout: 30 * time.Minute}); err != nil {
		s.Log.WarnContext(ctx, "enqueue import", slog.String("error", err.Error()))
	}
}

// HandleImport is the worker handler: parse (first run), validate, then
// create or update products for valid rows.
func (s *Service) HandleImport(ctx context.Context, payload []byte) error {
	var p ImportPayload
	if err := queue.Decode(payload, &p); err != nil {
		return err
	}
	err := s.runImport(ctx, p)
	if err != nil && queue.FinalAttempt(ctx) {
		// Do not leave the batch looking busy forever; Retry starts it again.
		pr := authctx.Principal{UserID: p.UserID}
		msg := "Lô nhập bị dừng do sự cố tạm thời. Hãy thử lại lô nhập."
		if ferr := s.failBatch(context.WithoutCancel(ctx), pr, p, msg); ferr != nil {
			s.Log.Error("import: mark failed", "batch_id", p.BatchID, "error", ferr)
		}
	}
	return err
}

func (s *Service) runImport(ctx context.Context, p ImportPayload) error {
	t := authctx.Tenant{OrgID: p.OrgID}
	pr := authctx.Principal{UserID: p.UserID}
	var batch productsdb.ProductImportBatch
	err := s.tx(ctx, pr, p.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		var err error
		if batch, err = q.LockBatch(ctx, productsdb.LockBatchParams{OrgID: p.OrgID, ID: p.BatchID}); err != nil {
			return err
		}
		status := BatchImporting
		if p.Parse {
			status = BatchParsing
		}
		return q.SetBatchStatus(ctx, productsdb.SetBatchStatusParams{Status: status, OrgID: p.OrgID, ID: p.BatchID})
	})
	if err != nil {
		return err
	}
	d := Defaults{}
	if batch.TemplateID != nil && batch.TemplateVersionID != nil && s.Templates != nil {
		secs, err := s.Templates.VersionSections(ctx, t, *batch.TemplateID, *batch.TemplateVersionID)
		if err != nil {
			return s.failBatch(ctx, pr, p, "Phiên bản mẫu shop của lô nhập này không còn tồn tại.")
		}
		d = DefaultsFromSections(secs)
	}
	if p.Parse {
		data, err := s.Store.Get(ctx, batch.FileStorageKey, MaxImportBytes)
		if err != nil {
			return err // retried by the queue
		}
		rows, perr := ParseFile(batch.Source, data)
		if perr != nil {
			return s.failBatch(ctx, pr, p, sentence(perr.Error()))
		}
		if err := s.insertRows(ctx, pr, p, rows); err != nil {
			return err
		}
	}
	if err := s.validateBatch(ctx, pr, p.OrgID, p.BatchID, d); err != nil {
		return err
	}
	if err := s.materialize(ctx, pr, t, p.BatchID, d); err != nil {
		return err
	}
	return s.finishBatch(ctx, pr, p)
}

func (s *Service) failBatch(ctx context.Context, pr authctx.Principal, p ImportPayload, msg string) error {
	return s.tx(ctx, pr, p.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		return q.SetBatchStatus(ctx, productsdb.SetBatchStatusParams{Status: BatchFailed, Error: &msg, OrgID: p.OrgID, ID: p.BatchID})
	})
}

func (s *Service) insertRows(ctx context.Context, pr authctx.Principal, p ImportPayload, rows []RawRow) error {
	return s.tx(ctx, pr, p.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		if err := q.DeleteRows(ctx, productsdb.DeleteRowsParams{OrgID: p.OrgID, BatchID: p.BatchID}); err != nil {
			return err
		}
		const chunk = 1000
		for start := 0; start < len(rows); start += chunk {
			end := min(start+chunk, len(rows))
			ids := make([]uuid.UUID, 0, end-start)
			nums := make([]int32, 0, end-start)
			raws := make([]string, 0, end-start)
			for i := start; i < end; i++ {
				raw, _ := json.Marshal(rows[i])
				ids = append(ids, db.NewID())
				nums = append(nums, int32(i+2)) // #nosec G115 -- ≤ 100k; row 1 is the header
				raws = append(raws, string(raw))
			}
			if err := q.InsertRows(ctx, productsdb.InsertRowsParams{OrgID: p.OrgID, BatchID: p.BatchID, Ids: ids, RowNumbers: nums, Raws: raws}); err != nil {
				return err
			}
		}
		return nil
	})
}

// validateBatch re-runs validation over every row that is not imported.
func (s *Service) validateBatch(ctx context.Context, pr authctx.Principal, orgID, batchID uuid.UUID, d Defaults) error {
	return s.tx(ctx, pr, orgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		all, err := q.AllRows(ctx, productsdb.AllRowsParams{OrgID: orgID, BatchID: batchID})
		if err != nil {
			return err
		}
		raws := make([]RawRow, len(all))
		for i, r := range all {
			_ = json.Unmarshal(r.Raw, &raws[i])
		}
		for _, g := range BuildGroups(raws, d) {
			for _, i := range g.Rows {
				r := all[i]
				if r.Status == "IMPORTED" {
					continue
				}
				status, errs := "VALID", g.Errors[i]
				if len(errs) > 0 {
					status = "INVALID"
				} else {
					errs = []RowError{}
				}
				eb, _ := json.Marshal(errs)
				if status == r.Status && bytes.Equal(eb, r.Errors) {
					continue
				}
				if err := q.UpdateRow(ctx, productsdb.UpdateRowParams{Raw: r.Raw, Status: status, Errors: eb, ProductID: r.ProductID, OrgID: orgID, ID: r.ID}); err != nil {
					return err
				}
			}
		}
		_, err = q.RefreshBatchCounts(ctx, productsdb.RefreshBatchCountsParams{OrgID: orgID, ID: batchID})
		return err
	})
}

// materialize creates or updates one product per valid group. Each group is
// its own transaction; images are downloaded before it.
func (s *Service) materialize(ctx context.Context, pr authctx.Principal, t authctx.Tenant, batchID uuid.UUID, d Defaults) error {
	var all []productsdb.ProductImportRow
	err := s.tx(ctx, pr, t.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		var err error
		all, err = q.AllRows(ctx, productsdb.AllRowsParams{OrgID: t.OrgID, BatchID: batchID})
		return err
	})
	if err != nil {
		return err
	}
	raws := make([]RawRow, len(all))
	for i, r := range all {
		_ = json.Unmarshal(r.Raw, &raws[i])
	}
	for _, g := range BuildGroups(raws, d) {
		ready := len(g.Errors) == 0
		for _, i := range g.Rows {
			ready = ready && all[i].Status == "VALID"
		}
		if !ready {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		s.importGroup(ctx, pr, t, batchID, g, all)
	}
	return nil
}

func (s *Service) importGroup(ctx context.Context, pr authctx.Principal, t authctx.Tenant, batchID uuid.UUID, g *Group, all []productsdb.ProductImportRow) {
	// Download and sanitize images first (outside any transaction).
	type img struct {
		data []byte
		url  string
	}
	var imgs []img
	var imgErr string
	for _, u := range g.ImageURLs {
		raw, err := s.Fetcher.Fetch(ctx, u)
		if err == nil {
			if err = s.scanFile(ctx, "image_urls", raw); err == nil {
				imgs = append(imgs, img{raw, u})
				continue
			}
		}
		imgErr = truncateStr(u, 80) + ": " + err.Error()
		break
	}
	setRows := func(q *productsdb.Queries, status string, errs []RowError, productID *uuid.UUID) error {
		eb, _ := json.Marshal(errs)
		for _, i := range g.Rows {
			r := all[i]
			if err := q.UpdateRow(ctx, productsdb.UpdateRowParams{Raw: r.Raw, Status: status, Errors: eb, ProductID: productID, OrgID: t.OrgID, ID: r.ID}); err != nil {
				return err
			}
		}
		return nil
	}
	err := s.tx(ctx, pr, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		if imgErr != "" {
			return setRows(q, "FAILED", []RowError{{Column: "image_urls", Code: "image_failed", Message: imgErr}}, nil)
		}
		existing, err := q.GetProductBySKU(ctx, productsdb.GetProductBySKUParams{OrgID: t.OrgID, Sku: g.Input.SKU})
		var cur *productsdb.Product
		switch {
		case err == nil:
			cur = &existing
		case !db.IsNoRows(err):
			return err
		}
		row, err := s.save(ctx, q, t.OrgID, &pr.UserID, cur, g.Input, "IMPORT", &batchID)
		if err != nil {
			if ae, ok := apperr.As(err); ok && ae.Kind == apperr.KindInvalid {
				var errs []RowError
				for _, f := range ae.Fields {
					errs = append(errs, RowError{Column: columnFor(f.Field), Code: f.Code, Message: f.Message})
				}
				return setRows(q, "FAILED", errs, nil)
			}
			return err
		}
		if err := q.DeleteImages(ctx, productsdb.DeleteImagesParams{OrgID: t.OrgID, ProductID: row.ID}); err != nil {
			return err
		}
		for _, im := range imgs {
			u := im.url
			if _, err := s.storeImage(ctx, q, t.OrgID, row.ID, im.data, &u); err != nil {
				if ae, ok := apperr.As(err); ok && ae.Kind == apperr.KindInvalid {
					return setRows(q, "FAILED", []RowError{{Column: "image_urls", Code: "image_failed", Message: truncateStr(u, 80) + ": " + ae.Fields[0].Message}}, nil)
				}
				return err
			}
		}
		id := row.ID
		if err := setRows(q, "IMPORTED", []RowError{}, &id); err != nil {
			return err
		}
		action := "product.imported"
		if cur != nil {
			action = "product.updated_by_import"
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &pr.UserID, Action: action,
			TargetType: "product", TargetID: &id, Metadata: map[string]any{"import_batch_id": batchID.String(), "sku": g.Input.SKU}})
	})
	if err != nil {
		s.Log.ErrorContext(ctx, "import group", slog.String("sku", g.Input.SKU), slog.String("error", err.Error()))
	}
}

func (s *Service) finishBatch(ctx context.Context, pr authctx.Principal, p ImportPayload) error {
	var rows []productsdb.ProductImportRow
	err := s.tx(ctx, pr, p.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		if _, err := q.RefreshBatchCounts(ctx, productsdb.RefreshBatchCountsParams{OrgID: p.OrgID, ID: p.BatchID}); err != nil {
			return err
		}
		var err error
		rows, err = q.AllRows(ctx, productsdb.AllRowsParams{OrgID: p.OrgID, BatchID: p.BatchID})
		return err
	})
	if err != nil {
		return err
	}
	report, err := buildReport(rows)
	if err != nil {
		return err
	}
	key := "orgs/" + p.OrgID.String() + "/imports/" + p.BatchID.String() + "/report.csv"
	if err := s.Store.Put(ctx, key, report, "text/csv"); err != nil {
		return err
	}
	return s.tx(ctx, pr, p.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		if err := q.SetBatchReport(ctx, productsdb.SetBatchReportParams{ReportStorageKey: &key, OrgID: p.OrgID, ID: p.BatchID}); err != nil {
			return err
		}
		return q.SetBatchStatus(ctx, productsdb.SetBatchStatusParams{Status: BatchCompleted, OrgID: p.OrgID, ID: p.BatchID})
	})
}

// buildReport writes the per-row outcome as CSV (FR-PRD-9).
func buildReport(rows []productsdb.ProductImportRow) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("\xEF\xBB\xBF") // BOM so spreadsheet apps read UTF-8
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"Dòng", "SKU", "Tên", "Trạng thái", "ID sản phẩm", "Lỗi"})
	for _, r := range rows {
		v := toRow(r)
		var msgs []string
		for _, e := range v.Errors {
			msgs = append(msgs, e.Column+": "+e.Message)
		}
		pid := ""
		if v.ProductID != nil {
			pid = v.ProductID.String()
		}
		_ = w.Write([]string{strconv.Itoa(v.RowNumber), csvSafe(v.Values["sku"]), csvSafe(v.Values["name"]), v.Status, pid, csvSafe(strings.Join(msgs, "; "))})
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

// ListBatches returns recent imports.
func (s *Service) ListBatches(ctx context.Context, p authctx.Principal, t authctx.Tenant) ([]Batch, error) {
	out := []Batch{}
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		bs, err := q.ListBatches(ctx, t.OrgID)
		for _, b := range bs {
			out = append(out, toBatch(b))
		}
		return err
	})
	return out, wrap(err, nil)
}

// GetBatch returns one import.
func (s *Service) GetBatch(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) (Batch, error) {
	var out Batch
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		b, err := q.GetBatch(ctx, productsdb.GetBatchParams{OrgID: t.OrgID, ID: id})
		out = toBatch(b)
		return wrap(err, errBatchNotFound)
	})
	return out, wrap(err, nil)
}

var rowStatuses = []string{"PENDING", "VALID", "INVALID", "IMPORTED", "FAILED"}

// ListRows pages through rows by row number.
func (s *Service) ListRows(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID, status *string, after, limit int) ([]Row, *string, error) {
	if status != nil {
		ok := false
		for _, st := range rowStatuses {
			ok = ok || st == *status
		}
		if !ok {
			return nil, nil, apperr.Invalid(apperr.FieldError{Field: "status", Code: "invalid", Message: "Trạng thái không hợp lệ."})
		}
	}
	out := []Row{}
	var next *string
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		if _, err := q.GetBatch(ctx, productsdb.GetBatchParams{OrgID: t.OrgID, ID: id}); err != nil {
			return wrap(err, errBatchNotFound)
		}
		rows, err := q.ListRows(ctx, productsdb.ListRowsParams{OrgID: t.OrgID, BatchID: id, Status: status, AfterRow: int32(after), Lim: int32(limit + 1)}) // #nosec G115 -- bounded
		for i, r := range rows {
			if i == limit {
				c := strconv.Itoa(int(rows[i-1].RowNumber))
				next = &c
				break
			}
			out = append(out, toRow(r))
		}
		return err
	})
	return out, next, wrap(err, nil)
}

// FixInput replaces cell values of a row.
type FixInput struct {
	Values map[string]string `json:"values"`
}

// FixRow updates a row and re-validates the batch. Imported rows are final.
func (s *Service) FixRow(ctx context.Context, p authctx.Principal, t authctx.Tenant, batchID, rowID uuid.UUID, in FixInput) (Row, error) {
	for k, v := range in.Values {
		if !knownColumn[k] {
			return Row{}, apperr.Invalid(apperr.FieldError{Field: "values." + k, Code: "unknown_column", Message: "Cột không hợp lệ."})
		}
		if len(v) > maxCellLen {
			return Row{}, apperr.Invalid(apperr.FieldError{Field: "values." + k, Code: "too_long", Message: "Giá trị quá dài."})
		}
	}
	var batch productsdb.ProductImportBatch
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		var err error
		if batch, err = q.LockBatch(ctx, productsdb.LockBatchParams{OrgID: t.OrgID, ID: batchID}); err != nil {
			return wrap(err, errBatchNotFound)
		}
		if batch.Status == BatchParsing || batch.Status == BatchImporting || batch.Status == BatchUploaded {
			return apperr.Conflict("import_running", "Lô nhập vẫn đang chạy. Hãy thử lại khi lô nhập hoàn tất.")
		}
		r, err := q.LockRow(ctx, productsdb.LockRowParams{OrgID: t.OrgID, BatchID: batchID, ID: rowID})
		if err != nil {
			return wrap(err, apperr.NotFound("Không tìm thấy dòng."))
		}
		if r.Status == "IMPORTED" {
			return apperr.Conflict("row_imported", "Dòng này đã được nhập. Hãy sửa trực tiếp sản phẩm.")
		}
		var raw RawRow
		_ = json.Unmarshal(r.Raw, &raw)
		if raw == nil {
			raw = RawRow{}
		}
		for k, v := range in.Values {
			v = strings.TrimSpace(v)
			if v == "" {
				delete(raw, k)
			} else {
				raw[k] = v
			}
		}
		rb, _ := json.Marshal(raw)
		if err := q.UpdateRow(ctx, productsdb.UpdateRowParams{Raw: rb, Status: "PENDING", Errors: []byte("[]"), OrgID: t.OrgID, ID: r.ID}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "import.row_fixed",
			TargetType: "import_batch", TargetID: &batchID, Metadata: map[string]any{"row": r.RowNumber}})
	})
	if err != nil {
		return Row{}, wrap(err, nil)
	}
	d, err := s.defaultsFor(ctx, t, batch)
	if err != nil {
		return Row{}, err
	}
	if err := s.validateBatch(ctx, p, t.OrgID, batchID, d); err != nil {
		return Row{}, wrap(err, nil)
	}
	var out Row
	err = s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		r, err := q.LockRow(ctx, productsdb.LockRowParams{OrgID: t.OrgID, BatchID: batchID, ID: rowID})
		out = toRow(r)
		return err
	})
	return out, wrap(err, nil)
}

func (s *Service) defaultsFor(ctx context.Context, t authctx.Tenant, b productsdb.ProductImportBatch) (Defaults, error) {
	if b.TemplateID == nil || b.TemplateVersionID == nil || s.Templates == nil {
		return Defaults{}, nil
	}
	secs, err := s.Templates.VersionSections(ctx, t, *b.TemplateID, *b.TemplateVersionID)
	if err != nil {
		return Defaults{}, apperr.Conflict("template_missing", "Phiên bản mẫu shop của lô nhập này không còn tồn tại.")
	}
	return DefaultsFromSections(secs), nil
}

// Retry imports rows that became valid (and re-tries failed ones).
func (s *Service) Retry(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) (Batch, error) {
	var out Batch
	var parse bool
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		b, err := q.LockBatch(ctx, productsdb.LockBatchParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errBatchNotFound)
		}
		if b.Status != BatchCompleted && b.Status != BatchFailed {
			return apperr.Conflict("import_running", "Chỉ có thể thử lại lô nhập đã kết thúc.")
		}
		all, err := q.AllRows(ctx, productsdb.AllRowsParams{OrgID: t.OrgID, BatchID: id})
		if err != nil {
			return err
		}
		// A file that was never read is parsed again.
		parse = len(all) == 0
		for _, r := range all {
			if r.Status == "FAILED" {
				if err := q.UpdateRow(ctx, productsdb.UpdateRowParams{Raw: r.Raw, Status: "PENDING", Errors: []byte("[]"), OrgID: t.OrgID, ID: r.ID}); err != nil {
					return err
				}
			}
		}
		status := BatchImporting
		if parse {
			status = BatchParsing
		}
		if err := q.SetBatchStatus(ctx, productsdb.SetBatchStatusParams{Status: status, OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		b.Status = status
		out = toBatch(b)
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "import.retried",
			TargetType: "import_batch", TargetID: &id})
	})
	if err != nil {
		return Batch{}, wrap(err, nil)
	}
	s.enqueueImport(ctx, ImportPayload{OrgID: t.OrgID, BatchID: id, UserID: p.UserID, Parse: parse})
	return out, nil
}

// ReportURL returns a short-lived download link for the import report.
func (s *Service) ReportURL(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) (string, time.Time, error) {
	b, err := s.batchRow(ctx, p, t, id)
	if err != nil {
		return "", time.Time{}, err
	}
	if b.ReportStorageKey == nil {
		return "", time.Time{}, apperr.Conflict("report_not_ready", "Báo cáo sẽ có khi lô nhập hoàn tất.")
	}
	ttl := 10 * time.Minute
	u, err := s.Store.PresignGet(ctx, *b.ReportStorageKey, ttl, fmt.Sprintf("import-%s-report.csv", id.String()[:8]))
	if err != nil {
		return "", time.Time{}, apperr.Internal(err)
	}
	return u, s.Clock.Now().Add(ttl).UTC(), nil
}

func (s *Service) batchRow(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) (productsdb.ProductImportBatch, error) {
	var b productsdb.ProductImportBatch
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		var err error
		b, err = q.GetBatch(ctx, productsdb.GetBatchParams{OrgID: t.OrgID, ID: id})
		return wrap(err, errBatchNotFound)
	})
	return b, wrap(err, nil)
}

// sentence turns an error string ("tệp không có dòng tiêu đề") into a
// user-facing sentence ("Tệp không có dòng tiêu đề.").
func sentence(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	if last := r[len(r)-1]; last != '.' && last != '!' && last != '?' {
		r = append(r, '.')
	}
	return string(r)
}
