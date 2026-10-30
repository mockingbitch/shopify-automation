package productpublish

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shopee-automation/internal/audit"
	"shopee-automation/internal/platform/apperr"
	"shopee-automation/internal/platform/authctx"
	"shopee-automation/internal/platform/db"
	"shopee-automation/internal/platform/httpx"
	"shopee-automation/internal/products"
	"shopee-automation/internal/shopee"
	"shopee-automation/internal/shops"
	"shopee-automation/internal/templates"
	"shopee-automation/internal/workflow"
	"shopee-automation/internal/workflow/workflowdb"
	"shopee-automation/internal/workflows/shopstep"
)

// Key is the definition key.
const Key = "PRODUCT_PUBLISH"

const (
	maxProducts = 200
	maxShops    = 50
)

// Input is the run input.
type Input struct {
	ProductIDs []uuid.UUID `json:"product_ids"`
	ItemStatus string      `json:"item_status"`
}

// Workflow holds the services the steps use.
type Workflow struct {
	Shops    *shops.Service
	Products *products.Service
	Engine   *workflow.Engine
	// VariationDelay is the wait after add_item before init_tier_variation
	// ("wait ≥ 5 s", product creation guide). Tests set it to zero.
	VariationDelay time.Duration
	ItemStatus     string
}

func (w *Workflow) input(sc *workflow.StepContext) (Input, error) {
	var in Input
	err := json.Unmarshal(sc.Input, &in)
	return in, err
}

// Definition returns PRODUCT_PUBLISH v1.
func (w *Workflow) Definition() workflow.Definition {
	return workflow.Definition{Key: Key, Version: 1, ValidateSelection: validateSelection, Steps: []workflow.StepSpec{
		{Key: "validate_connection", Capability: "shop.info.read", Timeout: time.Minute, Handler: shopstep.ValidateConnection(w.Shops)},
		{Key: "prepare_products", Capability: "product.attribute.read", Timeout: 5 * time.Minute, Handler: w.prepare},
		{Key: "approve_products", Timeout: time.Minute, Handler: w.approve},
		{Key: "create_products", Capability: "product.create", Mutating: true, Timeout: 5 * time.Minute,
			Handler: w.expand, ItemHandler: w.publishOne, ItemContinueOnFailure: true, ItemMaxAttempts: 6},
		{Key: "generate_report", Timeout: time.Minute, Report: true, Handler: w.report},
	}}
}

// Diff is the approval content.
type Diff struct {
	Changes []templates.Change `json:"changes"`
	Blocked []templates.Change `json:"blocked"`
}

func validateSelection(diff json.RawMessage, selected []string) error {
	var d Diff
	if err := json.Unmarshal(diff, &d); err != nil {
		return err
	}
	ok := map[string]bool{}
	for _, c := range d.Changes {
		ok[c.Path] = true
	}
	seen := map[string]bool{}
	for _, p := range selected {
		if !ok[p] || seen[p] {
			return fmt.Errorf("%s is not a product that can be published", p)
		}
		seen[p] = true
	}
	return nil
}

func (w *Workflow) approve(_ context.Context, sc *workflow.StepContext) workflow.Result {
	var prep Prepared
	if err := sc.Output("prepare_products", &prep); err != nil {
		return workflow.Permanent(err)
	}
	d := Diff{Changes: []templates.Change{}, Blocked: []templates.Change{}}
	for _, p := range prep.Plans {
		c := templates.Change{Path: "products." + p.ProductID.String(), Section: "products", Capability: "product.create", Mode: "API",
			Target: map[string]any{"sku": p.SKU, "name": p.Name, "price": p.Price + " " + p.Currency, "category_id": p.CategoryID}}
		if p.OK {
			c.Enabled, c.DefaultSelected = true, true
			c.Note = strings.Join(p.Warnings, " ")
			d.Changes = append(d.Changes, c)
		} else {
			c.Reason = "not_ready"
			c.Note = strings.Join(p.Problems, " ")
			d.Blocked = append(d.Blocked, c)
		}
	}
	if len(d.Changes) == 0 {
		return workflow.Skip("nothing_to_publish")
	}
	if sc.Approval != nil && sc.Approval.Status == "APPROVED" {
		return workflow.Succeeded(map[string]any{"selected": sc.Approval.Selected})
	}
	return workflow.AwaitApproval(d)
}

func (w *Workflow) expand(_ context.Context, sc *workflow.StepContext) workflow.Result {
	var ap struct {
		Selected []string `json:"selected"`
	}
	if raw := sc.Outputs["approve_products"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &ap)
	}
	items := make([]string, 0, len(ap.Selected))
	for _, p := range ap.Selected {
		items = append(items, strings.TrimPrefix(p, "products."))
	}
	sort.Strings(items)
	return workflow.Expand(items, map[string]int{"products": len(items)})
}

// ItemResult is the output of one product.
type ItemResult struct {
	ProductID string `json:"product_id"`
	SKU       string `json:"sku"`
	State     string `json:"state"`
	ItemID    string `json:"shopee_item_id"`
	Models    int    `json:"models"`
	Adopted   bool   `json:"adopted,omitempty"`
}

// publishOne runs the add_item protocol for one product (idempotent):
//
//	PLANNED/FAILED  → upload images → CREATING (committed) → add_item
//	  success       → CREATED
//	  ambiguous     → UNKNOWN, retry later
//	UNKNOWN/CREATING → search_item by item_sku: 1 match adopt, 0 create, >1 ask
//	CREATED         → variations (reconciled via get_model_list) → verify → VERIFIED
func (w *Workflow) publishOne(ctx context.Context, sc *workflow.StepContext) workflow.Result {
	pid, err := uuid.Parse(sc.ItemKey)
	if err != nil {
		return workflow.Permanent(err)
	}
	var prep Prepared
	if err := sc.Output("prepare_products", &prep); err != nil {
		return workflow.Permanent(err)
	}
	var pl *Plan
	for i := range prep.Plans {
		if prep.Plans[i].ProductID == pid {
			pl = &prep.Plans[i]
		}
	}
	if pl == nil || !pl.OK {
		return workflow.Permanent(errors.New("the product was not ready when approved"))
	}
	items, err := w.Products.LoadForPublish(ctx, sc.OrgID, []uuid.UUID{pid})
	if err != nil {
		return workflow.Retryable(err)
	}
	if len(items) == 0 {
		return workflow.Permanent(errors.New("the product was deleted"))
	}
	it := items[0]
	m, err := w.Products.EnsureMapping(ctx, sc.OrgID, sc.ShopID, pid, it.Product.SKU, sc.RunID)
	if err != nil {
		return workflow.Retryable(err)
	}
	in, _ := w.input(sc)
	res := ItemResult{ProductID: pid.String(), SKU: it.Product.SKU}

	var outcome workflow.Result
	err = shopstep.WithAccess(ctx, w.Shops, sc.OrgID, sc.ShopID, func(acc shops.Access) error {
		outcome = w.publishWithAccess(ctx, sc, acc, it, *pl, m, in, &res)
		return nil
	})
	if err != nil {
		return shopstep.FromErr(err)
	}
	return outcome
}

func (w *Workflow) publishWithAccess(ctx context.Context, sc *workflow.StepContext, acc shops.Access, it products.PublishItem, pl Plan,
	m products.MappingState, in Input, res *ItemResult) workflow.Result {
	setState := func(state string, itemID *int64, models any, lastErr any) error {
		return w.Products.SetMapping(ctx, sc.OrgID, m.ID, state, itemID, models, lastErr)
	}
	fail := func(err error) workflow.Result {
		r := shopstep.FromErr(err)
		if se, ok := shopee.AsError(err); ok && se.Kind != shopee.KindTransient && se.Kind != shopee.KindRateLimited &&
			se.Kind != shopee.KindAuth && se.Kind != shopee.KindAmbiguous {
			_ = setState(products.StateFailed, nil, nil, map[string]any{"code": se.Code, "message": se.Message, "request_id": se.RequestID})
		}
		return r
	}

	// 1. Reconcile an unclear earlier attempt by item_sku.
	if m.State == products.StateUnknown || m.State == products.StateCreating {
		ids, err := w.Shops.Shopee.SearchItemBySKU(ctx, acc.App, acc.AccessToken, acc.ShopID, it.Product.SKU)
		if err != nil {
			return fail(err)
		}
		switch len(ids) {
		case 0:
			sc.Log("info", "No item with SKU "+it.Product.SKU+" on Shopee after an unclear attempt; creating it.", nil)
			m.State = products.StatePlanned
		case 1:
			id := ids[0]
			if err := setState(products.StateCreated, &id, nil, nil); err != nil {
				return workflow.Retryable(err)
			}
			m.State, m.ItemID, res.Adopted = products.StateCreated, &id, true
			sc.Log("warn", "Adopted the existing Shopee item "+strconv.FormatInt(id, 10)+" for SKU "+it.Product.SKU+" after an unclear attempt.", nil)
		default:
			_ = setState(products.StateUnknown, nil, nil, map[string]any{"message": "several items share this SKU", "item_ids": ids})
			return workflow.NeedsUser("duplicate_items", fmt.Sprintf("Shopee has %d items with SKU %s (IDs %v). Keep one in Seller Centre, then resume.", len(ids), it.Product.SKU, ids))
		}
	}

	// 2. Create the item.
	if m.State == products.StatePlanned || m.State == products.StateFailed {
		imageIDs, r := w.uploadImages(ctx, sc, acc, it)
		if r != nil {
			return *r
		}
		item := addItemBody(it.Product, pl, imageIDs, in.ItemStatus)
		if err := setState(products.StateCreating, nil, nil, nil); err != nil {
			return workflow.Retryable(err)
		}
		id, meta, err := w.Shops.Shopee.AddItem(ctx, acc.App, acc.AccessToken, acc.ShopID, item)
		if err != nil {
			se, _ := shopee.AsError(err)
			if se != nil && (se.Kind == shopee.KindAmbiguous || se.Kind == shopee.KindTransient) {
				// Maybe created: the next attempt searches by SKU first.
				_ = setState(products.StateUnknown, nil, nil, map[string]any{"code": se.Code, "message": se.Message})
				return workflow.Retryable(fmt.Errorf("add_item outcome unknown (%s); will check by SKU", se.Code))
			}
			return fail(err)
		}
		if err := setState(products.StateCreated, &id, nil, nil); err != nil {
			return workflow.Retryable(err)
		}
		m.State, m.ItemID = products.StateCreated, &id
		sc.Log("info", "Created Shopee item "+strconv.FormatInt(id, 10)+" for SKU "+it.Product.SKU+".", map[string]any{"shopee_request_id": meta.RequestID})
	}

	// 3. Variations.
	var models []shopee.Model
	if m.State == products.StateCreated && len(it.Product.Tiers) > 0 {
		existing, err := w.Shops.Shopee.GetModelList(ctx, acc.App, acc.AccessToken, acc.ShopID, *m.ItemID)
		if err != nil {
			return fail(err)
		}
		if len(existing) > 0 {
			models = existing
		} else {
			if w.VariationDelay > 0 {
				select {
				case <-time.After(w.VariationDelay):
				case <-ctx.Done():
					return workflow.Retryable(ctx.Err())
				}
			}
			created, _, err := w.Shops.Shopee.InitTierVariation(ctx, acc.App, acc.AccessToken, acc.ShopID, tierBody(*m.ItemID, it.Product, pl))
			if err != nil {
				return fail(err)
			}
			models = created
		}
	}

	// 4. Verify.
	if m.State == products.StateCreated || m.State == products.StateVerified {
		list, err := w.Shops.Shopee.GetItemBaseInfo(ctx, acc.App, acc.AccessToken, acc.ShopID, []int64{*m.ItemID})
		if err != nil {
			return fail(err)
		}
		if len(list) != 1 || !strings.EqualFold(list[0].SKU, it.Product.SKU) {
			return workflow.Retryable(fmt.Errorf("item %d is not readable yet", *m.ItemID))
		}
		if len(it.Product.Tiers) > 0 && len(models) != len(it.Product.Variations) {
			sc.Log("warn", fmt.Sprintf("SKU %s has %d models on Shopee, %d expected.", it.Product.SKU, len(models), len(it.Product.Variations)), nil)
		}
		var mdl any
		if models != nil {
			mdl = models
		}
		if err := setState(products.StateVerified, m.ItemID, mdl, nil); err != nil {
			return workflow.Retryable(err)
		}
		res.State, res.ItemID, res.Models = products.StateVerified, strconv.FormatInt(*m.ItemID, 10), len(models)
	}
	return workflow.Succeeded(res)
}

// uploadImages uploads each image once per shop (reusing recorded IDs).
func (w *Workflow) uploadImages(ctx context.Context, sc *workflow.StepContext, acc shops.Access, it products.PublishItem) ([]string, *workflow.Result) {
	var ids []string
	for _, im := range it.Images {
		if id, ok, err := w.Products.ImageUpload(ctx, sc.OrgID, im.ID, sc.ShopID); err != nil {
			r := workflow.Retryable(err)
			return nil, &r
		} else if ok {
			ids = append(ids, id)
			continue
		}
		data, err := w.Products.ImageData(ctx, im.StorageKey)
		if err != nil {
			r := workflow.Retryable(err)
			return nil, &r
		}
		up, err := w.Shops.Shopee.UploadImage(ctx, acc.App, acc.AccessToken, acc.ShopID, data, im.ContentType, "normal")
		if err != nil {
			r := shopstep.FromErr(err)
			return nil, &r
		}
		if err := w.Products.RecordImageUpload(ctx, sc.OrgID, im.ID, sc.ShopID, up.ImageID, up.ImageURL); err != nil {
			r := workflow.Retryable(err)
			return nil, &r
		}
		ids = append(ids, up.ImageID)
	}
	return ids, nil
}

func toFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// addItemBody builds the add_item request (field names from the reference).
// Money is converted to Shopee's float only here.
func addItemBody(pr products.Product, pl Plan, imageIDs []string, status string) map[string]any {
	if status == "" {
		status = "UNLIST"
	}
	body := map[string]any{
		"original_price": toFloat(pr.Price.Amount),
		"description":    pr.Description,
		"weight":         toFloat(*pr.WeightKg),
		"item_name":      pr.Name,
		"item_status":    status,
		"logistic_info":  pl.Logistics,
		"category_id":    pl.CategoryID,
		"image":          map[string]any{"image_id_list": imageIDs},
		"item_sku":       pr.SKU,
		"seller_stock":   []any{map[string]any{"stock": pr.Stock}},
		"brand":          pl.Brand,
	}
	if len(pl.Attributes) > 0 {
		body["attribute_list"] = pl.Attributes
	}
	if pr.Condition != nil {
		body["condition"] = *pr.Condition
	}
	if pr.PackageCm != nil {
		body["dimension"] = map[string]any{"package_length": pr.PackageCm.Length, "package_width": pr.PackageCm.Width, "package_height": pr.PackageCm.Height}
	}
	return body
}

// tierBody builds init_tier_variation with standardised variations.
func tierBody(itemID int64, pr products.Product, pl Plan) map[string]any {
	var models []any
	for _, v := range pr.Variations {
		m := map[string]any{"tier_index": v.TierIndex, "original_price": toFloat(v.Price.Amount),
			"seller_stock": []any{map[string]any{"stock": v.Stock}}}
		if v.SKU != nil {
			m["model_sku"] = *v.SKU
		}
		models = append(models, m)
	}
	return map[string]any{"item_id": itemID, "model": models, "standardise_tier_variation": pl.StdTiers}
}

func (w *Workflow) report(_ context.Context, sc *workflow.StepContext) workflow.Result {
	var prep Prepared
	_ = sc.Output("prepare_products", &prep)
	var ap struct {
		Selected []string `json:"selected"`
	}
	_ = json.Unmarshal(sc.Outputs["approve_products"], &ap)
	created := []ItemResult{}
	for _, raw := range sc.ItemOutputs["create_products"] {
		var r ItemResult
		if json.Unmarshal(raw, &r) == nil {
			created = append(created, r)
		}
	}
	sort.Slice(created, func(i, j int) bool { return created[i].SKU < created[j].SKU })
	blocked := []map[string]any{}
	for _, p := range prep.Plans {
		if !p.OK {
			blocked = append(blocked, map[string]any{"sku": p.SKU, "problems": p.Problems})
		}
	}
	return workflow.Succeeded(map[string]any{
		"products": len(prep.Plans), "approved": len(ap.Selected), "published": len(created),
		"failed": len(ap.Selected) - len(created), "items": created, "blocked": blocked,
	})
}

// PublishInput chooses products (by ID or import batch) and shops.
type PublishInput struct {
	ProductIDs    []uuid.UUID `json:"product_ids"`
	ImportBatchID *uuid.UUID  `json:"import_batch_id"`
	ShopIDs       []uuid.UUID `json:"shop_ids"`
	ItemStatus    *string     `json:"item_status"`
}

// PublishResult is returned when runs were started.
type PublishResult struct {
	BatchID uuid.UUID   `json:"batch_id"`
	RunIDs  []uuid.UUID `json:"workflow_run_ids"`
	Replay  bool        `json:"replayed"`
}

// Start creates one PRODUCT_PUBLISH run per shop.
func (w *Workflow) Start(ctx context.Context, p authctx.Principal, t authctx.Tenant, in PublishInput, idemKey string, rec audit.Recorder) (PublishResult, error) {
	status := "UNLIST"
	if in.ItemStatus != nil {
		status = *in.ItemStatus
	}
	if status != "UNLIST" && status != "NORMAL" {
		return PublishResult{}, apperr.Invalid(apperr.FieldError{Field: "item_status", Code: "invalid", Message: "Must be UNLIST or NORMAL."})
	}
	if (len(in.ProductIDs) == 0) == (in.ImportBatchID == nil) {
		return PublishResult{}, apperr.Invalid(apperr.FieldError{Field: "product_ids", Code: "required", Message: "Give product_ids or import_batch_id."})
	}
	if len(in.ShopIDs) == 0 || len(in.ShopIDs) > maxShops {
		return PublishResult{}, apperr.Invalid(apperr.FieldError{Field: "shop_ids", Code: "invalid", Message: "Choose between 1 and 50 shops."})
	}
	ids := in.ProductIDs
	if in.ImportBatchID != nil {
		var err error
		if ids, err = w.Products.ProductIDsByBatch(ctx, t.OrgID, *in.ImportBatchID); err != nil {
			return PublishResult{}, apperr.Internal(err)
		}
		if len(ids) == 0 {
			return PublishResult{}, apperr.Invalid(apperr.FieldError{Field: "import_batch_id", Code: "empty", Message: "The import has no products."})
		}
	}
	if len(ids) > maxProducts {
		return PublishResult{}, apperr.Invalid(apperr.FieldError{Field: "product_ids", Code: "too_many", Message: "At most 200 products per publish."})
	}
	uniq := func(list []uuid.UUID) []uuid.UUID {
		seen := map[uuid.UUID]bool{}
		var out []uuid.UUID
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
		return out
	}
	ids, shopIDs := uniq(ids), uniq(in.ShopIDs)
	loaded, err := w.Products.LoadForPublish(ctx, t.OrgID, ids)
	if err != nil {
		return PublishResult{}, apperr.Internal(err)
	}
	if len(loaded) != len(ids) {
		return PublishResult{}, apperr.Invalid(apperr.FieldError{Field: "product_ids", Code: "not_found", Message: "Some products were not found."})
	}
	for _, sid := range shopIDs {
		d, err := w.Shops.Get(ctx, t, sid)
		if err != nil {
			if apperr.IsKind(err, apperr.KindNotFound) {
				return PublishResult{}, apperr.Invalid(apperr.FieldError{Field: "shop_ids", Code: "not_found", Message: "Shop " + sid.String() + " was not found."})
			}
			return PublishResult{}, err
		}
		if d.Shop.Status != shops.StatusActive {
			return PublishResult{}, apperr.FailedDependency("shop_reconnect_required", "Shop "+d.Shop.ShopeeShopID+" is not connected.")
		}
	}
	var key *string
	if idemKey != "" {
		if _, err := uuid.Parse(idemKey); err != nil {
			return PublishResult{}, apperr.BadRequest("bad_request", "Idempotency-Key must be a UUID.")
		}
		key = &idemKey
	}
	h := sha256.New()
	h.Write([]byte("publish|" + status))
	for _, id := range ids {
		h.Write([]byte("|p" + id.String()))
	}
	for _, id := range shopIDs {
		h.Write([]byte("|s" + id.String()))
	}
	hash := h.Sum(nil)

	var out PublishResult
	var started []uuid.UUID
	err = w.Engine.DB.Tx(ctx, db.Scope{OrgID: t.OrgID, UserID: p.UserID}, func(tx pgx.Tx) error {
		q := workflowdb.New(tx)
		if key != nil {
			if err := q.AdvisoryLock(ctx, "publish:"+t.OrgID.String()+":"+*key); err != nil {
				return err
			}
			prev, err := q.RunsByIdempotencyKey(ctx, workflowdb.RunsByIdempotencyKeyParams{OrgID: t.OrgID, IdempotencyKey: key})
			if err != nil {
				return err
			}
			if len(prev) > 0 {
				if !bytes.Equal(prev[0].RequestHash, hash) {
					return apperr.Conflict("idempotency_mismatch", "This Idempotency-Key was used for a different request.")
				}
				out.Replay = true
				for _, r := range prev {
					out.RunIDs = append(out.RunIDs, r.ID)
					if r.BatchID != nil {
						out.BatchID = *r.BatchID
					}
				}
				return nil
			}
		}
		batch := db.NewID()
		specs := make([]workflow.StartSpec, 0, len(shopIDs))
		for _, sid := range shopIDs {
			specs = append(specs, workflow.StartSpec{Definition: Key, ShopID: sid, Input: Input{ProductIDs: ids, ItemStatus: status}})
		}
		runs, err := w.Engine.CreateRuns(ctx, tx, workflow.StartOptions{OrgID: t.OrgID, UserID: p.UserID, BatchID: &batch,
			IdempotencyKey: key, RequestHash: hash}, specs)
		if err != nil {
			return err
		}
		out.BatchID = batch
		for _, r := range runs {
			out.RunIDs = append(out.RunIDs, r.ID)
			started = append(started, r.ID)
		}
		return rec.Record(ctx, tx, audit.Entry{OrgID: &t.OrgID, ActorUserID: &p.UserID, Action: "products.publish_started",
			TargetType: "workflow_batch", TargetID: &batch, Metadata: map[string]any{"products": len(ids), "shops": len(shopIDs)}})
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return PublishResult{}, err
		}
		return PublishResult{}, apperr.Internal(err)
	}
	w.Engine.Kick(ctx, t.OrgID, started...)
	return out, nil
}

// Handler serves the publish endpoint.
type Handler struct {
	Workflow *Workflow
	Audit    audit.Recorder
}

// Publish handles POST /orgs/{orgID}/products/publish.
func (h Handler) Publish(w http.ResponseWriter, r *http.Request) error {
	var in PublishInput
	if err := httpx.Decode(w, r, &in); err != nil {
		return err
	}
	out, err := h.Workflow.Start(r.Context(), authctx.MustPrincipal(r.Context()), authctx.MustTenant(r.Context()), in,
		r.Header.Get("Idempotency-Key"), h.Audit)
	if err != nil {
		return err
	}
	return httpx.JSON(w, http.StatusAccepted, httpx.Data[PublishResult]{Data: out})
}
