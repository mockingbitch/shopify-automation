package products

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/clock"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/media"
	"shopee-automation/internal/platform/queue"
	"shopee-automation/internal/platform/scan"
	"shopee-automation/internal/platform/storage"
	"shopee-automation/internal/products/productsdb"
)

const imageURLTTL = 15 * time.Minute

// Service implements product and import use cases.
type Service struct {
	DB      *db.DB
	Clock   clock.Clock
	Audit   audit.Recorder
	Store   storage.Store
	Fetcher *media.Fetcher
	Queue   queue.Enqueuer
	Log     *slog.Logger
	// Templates resolves template defaults for imports (optional).
	Templates TemplateDefaults
	// Scanner checks uploads for malware (optional).
	Scanner scan.Scanner
}

// scanFile rejects infected uploads. With a scanner configured but
// unreachable, the upload is refused (fail closed).
func (s *Service) scanFile(ctx context.Context, field string, data []byte) error {
	if s.Scanner == nil {
		return nil
	}
	err := s.Scanner.Scan(ctx, data)
	if errors.Is(err, scan.ErrInfected) {
		s.Log.WarnContext(ctx, "upload rejected by the malware scanner", slog.String("error", err.Error()))
		return apperr.Invalid(apperr.FieldError{Field: field, Code: "malware", Message: "Tệp bị từ chối vì trình quét mã độc phát hiện nguy cơ."})
	}
	if err != nil {
		s.Log.ErrorContext(ctx, "malware scanner unavailable", slog.String("error", err.Error()))
		return apperr.Unavailable("scanner_unavailable", "Hiện chưa thể kiểm tra tệp. Hãy thử lại sau vài phút.")
	}
	return nil
}

func (s *Service) tx(ctx context.Context, p authctx.Principal, orgID uuid.UUID, fn func(q *productsdb.Queries, tx pgx.Tx) error) error {
	return s.DB.Tx(ctx, db.Scope{OrgID: orgID, UserID: p.UserID}, func(tx pgx.Tx) error { return fn(productsdb.New(tx), tx) })
}

var errNotFound = apperr.NotFound("Không tìm thấy sản phẩm.")

// Money is a decimal amount with its currency.
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// Image is a stored product image.
type Image struct {
	ID       uuid.UUID `json:"id"`
	Position int       `json:"position"`
	URL      string    `json:"url"`
	Width    int       `json:"width"`
	Height   int       `json:"height"`
	SHA256   string    `json:"sha256"`
}

// Variation is one model.
type Variation struct {
	ID        uuid.UUID `json:"id"`
	TierIndex []int     `json:"tier_index"`
	SKU       *string   `json:"sku"`
	Price     Money     `json:"price"`
	Stock     int       `json:"stock"`
}

// Mapping is the publish state of a product in one shop.
type Mapping struct {
	ShopID       uuid.UUID       `json:"shop_id"`
	PublishState string          `json:"publish_state"`
	ShopeeItemID *string         `json:"shopee_item_id"`
	ItemSKU      string          `json:"item_sku"`
	LastError    json.RawMessage `json:"last_error"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// Product is the API view.
type Product struct {
	ID               uuid.UUID   `json:"id"`
	SKU              string      `json:"sku"`
	Name             string      `json:"name"`
	Description      string      `json:"description"`
	ShopeeCategoryID *string     `json:"shopee_category_id"`
	CategoryLabel    *string     `json:"category_label"`
	BrandName        *string     `json:"brand_name"`
	Condition        *string     `json:"condition"`
	Attributes       []Attribute `json:"attributes"`
	Price            Money       `json:"price"`
	Stock            int         `json:"stock"`
	WeightKg         *string     `json:"weight_kg"`
	PackageCm        *Package    `json:"package_cm"`
	DaysToShip       *int        `json:"days_to_ship"`
	Tiers            []Tier      `json:"tiers"`
	Variations       []Variation `json:"variations"`
	Images           []Image     `json:"images"`
	Mappings         []Mapping   `json:"mappings"`
	Source           string      `json:"source"`
	ImportBatchID    *uuid.UUID  `json:"import_batch_id"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

// ETag is the product's entity tag.
func (p Product) ETag() string { return fmt.Sprintf(`W/"%d"`, p.UpdatedAt.UnixMicro()) }

func int64Str(v *int64) *string {
	if v == nil {
		return nil
	}
	s := strconv.FormatInt(*v, 10)
	return &s
}

func toProduct(r productsdb.Product) Product {
	p := Product{ID: r.ID, SKU: r.Sku, Name: r.Name, Description: r.Description, ShopeeCategoryID: int64Str(r.ShopeeCategoryID),
		CategoryLabel: r.CategoryLabel, BrandName: r.BrandName, Condition: r.Condition, Price: Money{r.Price, r.Currency},
		Stock: int(r.Stock), WeightKg: r.WeightKg, Source: r.Source, ImportBatchID: r.ImportBatchID,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(), Attributes: []Attribute{}, Tiers: []Tier{},
		Variations: []Variation{}, Images: []Image{}, Mappings: []Mapping{}}
	_ = json.Unmarshal(r.Attributes, &p.Attributes)
	_ = json.Unmarshal(r.Tiers, &p.Tiers)
	if len(r.PackageCm) > 0 {
		var pk Package
		if json.Unmarshal(r.PackageCm, &pk) == nil {
			p.PackageCm = &pk
		}
	}
	if r.DaysToShip != nil {
		d := int(*r.DaysToShip)
		p.DaysToShip = &d
	}
	return p
}

// enrich loads variations, images (with presigned URLs) and mappings.
func (s *Service) enrich(ctx context.Context, q *productsdb.Queries, orgID uuid.UUID, ps []Product, withMappings bool) error {
	if len(ps) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(ps))
	idx := map[uuid.UUID]int{}
	for i, p := range ps {
		ids[i], idx[p.ID] = p.ID, i
	}
	vs, err := q.ListVariations(ctx, productsdb.ListVariationsParams{OrgID: orgID, ProductIds: ids})
	if err != nil {
		return err
	}
	for _, v := range vs {
		ti := make([]int, len(v.TierIndex))
		for i, x := range v.TierIndex {
			ti[i] = int(x)
		}
		p := &ps[idx[v.ProductID]]
		p.Variations = append(p.Variations, Variation{ID: v.ID, TierIndex: ti, SKU: v.Sku, Price: Money{v.Price, p.Price.Currency}, Stock: int(v.Stock)})
	}
	imgs, err := q.ListImages(ctx, productsdb.ListImagesParams{OrgID: orgID, ProductIds: ids})
	if err != nil {
		return err
	}
	for _, im := range imgs {
		u, err := s.Store.PresignGet(ctx, im.StorageKey, imageURLTTL, "")
		if err != nil {
			return err
		}
		p := &ps[idx[im.ProductID]]
		p.Images = append(p.Images, Image{ID: im.ID, Position: int(im.Position), URL: u, Width: int(im.Width), Height: int(im.Height),
			SHA256: hex.EncodeToString(im.Sha256)})
	}
	if !withMappings {
		return nil
	}
	ms, err := q.ListMappings(ctx, productsdb.ListMappingsParams{OrgID: orgID, ProductIds: ids})
	for _, m := range ms {
		p := &ps[idx[m.ProductID]]
		var errJSON json.RawMessage
		if len(m.LastError) > 0 {
			errJSON = m.LastError
		}
		p.Mappings = append(p.Mappings, Mapping{ShopID: m.ShopConnectionID, PublishState: m.PublishState, ShopeeItemID: int64Str(m.ShopeeItemID),
			ItemSKU: m.ItemSku, LastError: errJSON, UpdatedAt: m.UpdatedAt.UTC()})
	}
	return err
}

// Get returns one product with everything.
func (s *Service) Get(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) (Product, error) {
	var out Product
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		r, err := q.GetProduct(ctx, productsdb.GetProductParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errNotFound)
		}
		list := []Product{toProduct(r)}
		if err := s.enrich(ctx, q, t.OrgID, list, true); err != nil {
			return err
		}
		out = list[0]
		return nil
	})
	return out, wrap(err, nil)
}

// ListFilter narrows the product list.
type ListFilter struct {
	Q       *string
	BatchID *uuid.UUID
	Limit   int
	Cursor  string
}

// List returns products, newest first.
func (s *Service) List(ctx context.Context, p authctx.Principal, t authctx.Tenant, f ListFilter) ([]Product, *string, error) {
	var before *time.Time
	var beforeID *uuid.UUID
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		ts, idPart, ok := strings.Cut(string(raw), "|")
		at, terr := time.Parse(time.RFC3339Nano, ts)
		id, ierr := uuid.Parse(idPart)
		if err != nil || !ok || terr != nil || ierr != nil {
			return nil, nil, apperr.Invalid(apperr.FieldError{Field: "cursor", Code: "invalid", Message: "Con trỏ phân trang không hợp lệ. Hãy tải lại danh sách."})
		}
		before, beforeID = &at, &id
	}
	if f.Q != nil {
		q := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(*f.Q))
		f.Q = &q
	}
	out := []Product{}
	var next *string
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		rows, err := q.ListProducts(ctx, productsdb.ListProductsParams{OrgID: t.OrgID, Q: f.Q, BatchID: f.BatchID, Before: before,
			BeforeID: beforeID, Lim: int32(f.Limit + 1)}) // #nosec G115 -- bounded by QueryInt
		if err != nil {
			return err
		}
		for i, r := range rows {
			if i == f.Limit {
				last := rows[i-1]
				c := base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String()))
				next = &c
				break
			}
			out = append(out, toProduct(r))
		}
		return s.enrich(ctx, q, t.OrgID, out, true)
	})
	return out, next, wrap(err, nil)
}

func nullJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func parseInt64Ptr(s *string) *int64 {
	if s == nil {
		return nil
	}
	v, err := strconv.ParseInt(*s, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

// save writes a validated input (create or replace) in a transaction.
func (s *Service) save(ctx context.Context, q *productsdb.Queries, orgID uuid.UUID, userID *uuid.UUID, existing *productsdb.Product,
	in Input, source string, batch *uuid.UUID) (productsdb.Product, error) {
	attrs := in.Attributes
	if attrs == nil {
		attrs = []Attribute{}
	}
	tiers := in.Tiers
	if tiers == nil {
		tiers = []Tier{}
	}
	var pkg []byte
	if in.PackageCm != nil {
		pkg = nullJSON(in.PackageCm)
	}
	var dts *int32
	if in.DaysToShip != nil {
		d := int32(*in.DaysToShip) // #nosec G115 -- validated 1..90
		dts = &d
	}
	var row productsdb.Product
	var err error
	if existing == nil {
		row, err = q.InsertProduct(ctx, productsdb.InsertProductParams{ID: db.NewID(), OrgID: orgID, Sku: in.SKU, Name: in.Name,
			Description: in.Description, ShopeeCategoryID: parseInt64Ptr(in.ShopeeCategoryID), CategoryLabel: in.CategoryLabel,
			BrandName: in.BrandName, Condition: in.Condition, Attributes: nullJSON(attrs), Price: in.Price, Currency: in.Currency,
			Stock: int32(in.Stock), WeightKg: in.WeightKg, PackageCm: pkg, DaysToShip: dts, Tiers: nullJSON(tiers), // #nosec G115 -- validated
			Source: source, ImportBatchID: batch, CreatedBy: userID})
	} else {
		row, err = q.UpdateProduct(ctx, productsdb.UpdateProductParams{Sku: in.SKU, Name: in.Name, Description: in.Description,
			ShopeeCategoryID: parseInt64Ptr(in.ShopeeCategoryID), CategoryLabel: in.CategoryLabel, BrandName: in.BrandName,
			Condition: in.Condition, Attributes: nullJSON(attrs), Price: in.Price, Currency: in.Currency, Stock: int32(in.Stock), // #nosec G115
			WeightKg: in.WeightKg, PackageCm: pkg, DaysToShip: dts, Tiers: nullJSON(tiers), ImportBatchID: batch,
			OrgID: orgID, ID: existing.ID})
	}
	if err != nil {
		if db.UniqueViolation(err) == "products_sku_uk" {
			return row, apperr.Invalid(apperr.FieldError{Field: "sku", Code: "taken", Message: "SKU này đã được dùng cho một sản phẩm khác."})
		}
		return row, err
	}
	if err := q.DeleteVariations(ctx, productsdb.DeleteVariationsParams{OrgID: orgID, ProductID: row.ID}); err != nil {
		return row, err
	}
	for _, v := range in.Variations {
		ti := make([]int32, len(v.TierIndex))
		for i, x := range v.TierIndex {
			ti[i] = int32(x) // #nosec G115 -- validated < 50
		}
		if err := q.InsertVariation(ctx, productsdb.InsertVariationParams{ID: db.NewID(), OrgID: orgID, ProductID: row.ID, TierIndex: ti,
			Sku: v.SKU, Price: v.Price, Stock: int32(v.Stock)}); err != nil { // #nosec G115
			if db.UniqueViolation(err) != "" {
				return row, apperr.Invalid(apperr.FieldError{Field: "variations", Code: "duplicate", Message: "Các phân loại hàng không được trùng nhau."})
			}
			return row, err
		}
	}
	return row, nil
}

// Create adds a product.
func (s *Service) Create(ctx context.Context, p authctx.Principal, t authctx.Tenant, in Input) (Product, error) {
	if errs := Validate(&in); len(errs) > 0 {
		return Product{}, apperr.Invalid(errs...)
	}
	var id uuid.UUID
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		row, err := s.save(ctx, q, t.OrgID, &p.UserID, nil, in, "MANUAL", nil)
		if err != nil {
			return err
		}
		id = row.ID
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "product.created",
			TargetType: "product", TargetID: &id, Metadata: map[string]any{"sku": in.SKU}})
	})
	if err != nil {
		return Product{}, wrap(err, nil)
	}
	return s.Get(ctx, p, t, id)
}

// Replace overwrites a product (If-Match required). Images are kept.
func (s *Service) Replace(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID, in Input, ifMatch string) (Product, error) {
	if ifMatch == "" {
		return Product{}, apperr.PreconditionFailed("Thiếu header If-Match.")
	}
	if errs := Validate(&in); len(errs) > 0 {
		return Product{}, apperr.Invalid(errs...)
	}
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		cur, err := q.LockProduct(ctx, productsdb.LockProductParams{OrgID: t.OrgID, ID: id})
		if err != nil {
			return wrap(err, errNotFound)
		}
		if toProduct(cur).ETag() != ifMatch {
			return apperr.PreconditionFailed("Sản phẩm vừa được người khác thay đổi. Hãy tải lại và thử lại.")
		}
		if _, err := s.save(ctx, q, t.OrgID, &p.UserID, &cur, in, cur.Source, nil); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "product.updated",
			TargetType: "product", TargetID: &id})
	})
	if err != nil {
		return Product{}, wrap(err, nil)
	}
	return s.Get(ctx, p, t, id)
}

// Delete soft-deletes a product. Items already on Shopee are not deleted.
func (s *Service) Delete(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID) error {
	return wrap(s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		if _, err := q.LockProduct(ctx, productsdb.LockProductParams{OrgID: t.OrgID, ID: id}); err != nil {
			return wrap(err, errNotFound)
		}
		if err := q.SoftDeleteProduct(ctx, productsdb.SoftDeleteProductParams{Now: s.Clock.Now(), OrgID: t.OrgID, ID: id}); err != nil {
			return err
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "product.deleted",
			TargetType: "product", TargetID: &id})
	}), nil)
}

// storeImage sanitizes and stores an image for a product (inside q's tx).
func (s *Service) storeImage(ctx context.Context, q *productsdb.Queries, orgID, productID uuid.UUID, raw []byte, sourceURL *string) (productsdb.ProductImage, error) {
	n, err := q.CountImages(ctx, productsdb.CountImagesParams{OrgID: orgID, ProductID: productID})
	if err != nil {
		return productsdb.ProductImage{}, err
	}
	if n >= maxImages {
		return productsdb.ProductImage{}, apperr.Invalid(apperr.FieldError{Field: "file", Code: "too_many", Message: "Mỗi sản phẩm có tối đa 20 hình ảnh."})
	}
	img, err := media.Sanitize(raw)
	if err != nil {
		return productsdb.ProductImage{}, apperr.Invalid(apperr.FieldError{Field: "file", Code: "invalid_image", Message: sentence(err.Error())})
	}
	id := db.NewID()
	ext := "jpg"
	if img.ContentType == "image/png" {
		ext = "png"
	}
	key := "orgs/" + orgID.String() + "/products/" + productID.String() + "/" + id.String() + "." + ext
	if err := s.Store.Put(ctx, key, img.Data, img.ContentType); err != nil {
		return productsdb.ProductImage{}, err
	}
	return q.InsertImage(ctx, productsdb.InsertImageParams{ID: id, OrgID: orgID, ProductID: productID, Position: n,
		StorageKey: key, ContentType: img.ContentType, Sha256: img.SHA256[:], Width: int32(img.Width), Height: int32(img.Height), // #nosec G115 -- ≤ 40 MP
		SourceUrl: sourceURL})
}

// AddImage uploads one image to a product.
func (s *Service) AddImage(ctx context.Context, p authctx.Principal, t authctx.Tenant, id uuid.UUID, raw []byte) (Image, error) {
	if err := s.scanFile(ctx, "file", raw); err != nil {
		return Image{}, err
	}
	var out Image
	err := s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		if _, err := q.LockProduct(ctx, productsdb.LockProductParams{OrgID: t.OrgID, ID: id}); err != nil {
			return wrap(err, errNotFound)
		}
		im, err := s.storeImage(ctx, q, t.OrgID, id, raw, nil)
		if err != nil {
			return err
		}
		u, err := s.Store.PresignGet(ctx, im.StorageKey, imageURLTTL, "")
		if err != nil {
			return err
		}
		out = Image{ID: im.ID, Position: int(im.Position), URL: u, Width: int(im.Width), Height: int(im.Height), SHA256: hex.EncodeToString(im.Sha256)}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "product.image_added",
			TargetType: "product", TargetID: &id, Metadata: map[string]any{"image_id": im.ID.String()}})
	})
	return out, wrap(err, nil)
}

// DeleteImage removes an image from a product.
func (s *Service) DeleteImage(ctx context.Context, p authctx.Principal, t authctx.Tenant, id, imageID uuid.UUID) error {
	return wrap(s.tx(ctx, p, t.OrgID, func(q *productsdb.Queries, tx pgx.Tx) error {
		n, err := q.DeleteImage(ctx, productsdb.DeleteImageParams{OrgID: t.OrgID, ProductID: id, ID: imageID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.NotFound("Không tìm thấy hình ảnh.")
		}
		return s.Audit.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "product.image_removed",
			TargetType: "product", TargetID: &id, Metadata: map[string]any{"image_id": imageID.String()}})
	}), nil)
}

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
