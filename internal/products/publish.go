package products

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/media"
	"shopee-automation/internal/products/productsdb"
)

// Publish states of a product in a shop (workflow-design.md §5).
const (
	StatePlanned  = "PLANNED"
	StateCreating = "CREATING"
	StateCreated  = "CREATED"
	StateVerified = "VERIFIED"
	StateFailed   = "FAILED"
	StateUnknown  = "UNKNOWN"
)

// PublishImage is an image as the publisher needs it.
type PublishImage struct {
	ID          uuid.UUID
	StorageKey  string
	ContentType string
}

// PublishItem is a product with what publishing needs.
type PublishItem struct {
	Product Product
	Images  []PublishImage
}

// LoadForPublish reads products (skipping deleted ones) for a workflow.
func (s *Service) LoadForPublish(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]PublishItem, error) {
	var out []PublishItem
	err := s.tx(ctx, authctx.Principal{}, orgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		rows, err := q.ProductsByIDs(ctx, productsdb.ProductsByIDsParams{OrgID: orgID, Ids: ids})
		if err != nil {
			return err
		}
		ps := make([]Product, len(rows))
		pids := make([]uuid.UUID, len(rows))
		for i, r := range rows {
			ps[i], pids[i] = toProduct(r), r.ID
		}
		vs, err := q.ListVariations(ctx, productsdb.ListVariationsParams{OrgID: orgID, ProductIds: pids})
		if err != nil {
			return err
		}
		imgs, err := q.ListImages(ctx, productsdb.ListImagesParams{OrgID: orgID, ProductIds: pids})
		if err != nil {
			return err
		}
		byID := map[uuid.UUID]int{}
		for i, p := range ps {
			byID[p.ID] = i
			out = append(out, PublishItem{Product: p})
		}
		for _, v := range vs {
			ti := make([]int, len(v.TierIndex))
			for i, x := range v.TierIndex {
				ti[i] = int(x)
			}
			it := &out[byID[v.ProductID]]
			it.Product.Variations = append(it.Product.Variations, Variation{ID: v.ID, TierIndex: ti, SKU: v.Sku,
				Price: Money{v.Price, it.Product.Price.Currency}, Stock: int(v.Stock)})
		}
		for _, im := range imgs {
			it := &out[byID[im.ProductID]]
			it.Images = append(it.Images, PublishImage{ID: im.ID, StorageKey: im.StorageKey, ContentType: im.ContentType})
		}
		return nil
	})
	return out, err
}

// ImageData reads a stored image.
func (s *Service) ImageData(ctx context.Context, key string) ([]byte, error) {
	return s.Store.Get(ctx, key, media.MaxBytes)
}

// ProductIDsByBatch lists the products created or updated by an import.
func (s *Service) ProductIDsByBatch(ctx context.Context, orgID, batchID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := s.tx(ctx, authctx.Principal{}, orgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		var err error
		ids, err = q.ProductIDsByBatch(ctx, productsdb.ProductIDsByBatchParams{OrgID: orgID, BatchID: &batchID})
		return err
	})
	return ids, err
}

// MappingState is the publish state of one product in one shop.
type MappingState struct {
	ID     uuid.UUID
	State  string
	ItemID *int64
	SKU    string
	Models json.RawMessage
}

// EnsureMapping creates (or returns) the mapping row. The idempotency key
// is stable per (org, shop, product).
func (s *Service) EnsureMapping(ctx context.Context, orgID, shopID, productID uuid.UUID, sku string, runID uuid.UUID) (MappingState, error) {
	sum := sha256.Sum256([]byte(orgID.String() + "|" + shopID.String() + "|" + productID.String()))
	var out MappingState
	err := s.tx(ctx, authctx.Principal{}, orgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		m, err := q.InsertMapping(ctx, productsdb.InsertMappingParams{ID: db.NewID(), OrgID: orgID, ShopConnectionID: shopID,
			ProductID: productID, IdempotencyKey: hex.EncodeToString(sum[:]), ItemSku: sku, LastRunID: &runID})
		out = MappingState{ID: m.ID, State: m.PublishState, ItemID: m.ShopeeItemID, SKU: m.ItemSku, Models: m.Models}
		return err
	})
	return out, err
}

// SetMapping records a new publish state. Commit before calling Shopee so
// a crash leaves a state the next attempt can reconcile from.
func (s *Service) SetMapping(ctx context.Context, orgID, mappingID uuid.UUID, state string, itemID *int64, models any, lastErr any) error {
	var mb, eb []byte
	if models != nil {
		mb, _ = json.Marshal(models)
	}
	if lastErr != nil {
		eb, _ = json.Marshal(lastErr)
	}
	return s.tx(ctx, authctx.Principal{}, orgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		_, err := q.SetMappingState(ctx, productsdb.SetMappingStateParams{PublishState: state, ShopeeItemID: itemID, Models: mb,
			LastError: eb, OrgID: orgID, ID: mappingID})
		return err
	})
}

// ImageUpload returns the Shopee image ID of an image in a shop, if uploaded.
func (s *Service) ImageUpload(ctx context.Context, orgID, imageID, shopID uuid.UUID) (string, bool, error) {
	var id string
	var found bool
	err := s.tx(ctx, authctx.Principal{}, orgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		u, err := q.GetImageUpload(ctx, productsdb.GetImageUploadParams{OrgID: orgID, ProductImageID: imageID, ShopConnectionID: shopID})
		if db.IsNoRows(err) {
			return nil
		}
		id, found = u.ShopeeImageID, err == nil
		return err
	})
	return id, found, err
}

// RecordImageUpload stores a Shopee image ID for reuse.
func (s *Service) RecordImageUpload(ctx context.Context, orgID, imageID, shopID uuid.UUID, shopeeID, url string) error {
	return s.tx(ctx, authctx.Principal{}, orgID, func(q *productsdb.Queries, _ pgx.Tx) error {
		var u *string
		if url != "" {
			u = &url
		}
		return q.InsertImageUpload(ctx, productsdb.InsertImageUploadParams{ID: db.NewID(), OrgID: orgID, ProductImageID: imageID,
			ShopConnectionID: shopID, ShopeeImageID: shopeeID, ShopeeImageUrl: u})
	})
}
