package shopee

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"shopee-automation/internal/shopee/signer"
)

// Product API paths (v2 reference, read 2026-10-30). Open questions about
// parameter encoding are tracked as S-10 in docs/shopee-integration.md.
const (
	PathGetCategory       = "/api/v2/product/get_category"
	PathGetAttributeTree  = "/api/v2/product/get_attribute_tree"
	PathGetBrandList      = "/api/v2/product/get_brand_list"
	PathGetItemLimit      = "/api/v2/product/get_item_limit"
	PathGetVariationTree  = "/api/v2/product/get_variation_tree"
	PathUploadImage       = "/api/v2/media_space/upload_image"
	PathAddItem           = "/api/v2/product/add_item"
	PathInitTierVariation = "/api/v2/product/init_tier_variation"
	PathSearchItem        = "/api/v2/product/search_item"
	PathGetItemBaseInfo   = "/api/v2/product/get_item_base_info"
	PathGetModelList      = "/api/v2/product/get_model_list"
)

func ids(list []int64) string {
	s := make([]string, len(list))
	for i, v := range list {
		s[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(s, ",")
}

func shopGet(path string, token string, shopID int64, q url.Values) Request {
	return Request{Method: "GET", Path: path, Kind: signer.Shop, AccessToken: token, TargetID: shopID, Query: q, Idempotent: true}
}

// Category is one entry of get_category.
type Category struct {
	ID          int64  `json:"category_id"`
	ParentID    int64  `json:"parent_category_id"`
	Name        string `json:"original_category_name"`
	DisplayName string `json:"display_category_name"`
	HasChildren bool   `json:"has_children"`
}

// GetCategories returns the shop's category tree as a flat list.
func (c *Client) GetCategories(ctx context.Context, app App, token string, shopID int64) ([]Category, error) {
	var out struct {
		Response struct {
			List []Category `json:"category_list"`
		} `json:"response"`
	}
	_, err := c.Do(ctx, app, shopGet(PathGetCategory, token, shopID, nil), &out)
	return out.Response.List, err
}

// Attribute is one node of get_attribute_tree.
type Attribute struct {
	ID        int64  `json:"attribute_id"`
	Mandatory bool   `json:"mandatory"`
	Name      string `json:"name"`
	Values    []struct {
		ID   int64  `json:"value_id"`
		Name string `json:"name"`
	} `json:"attribute_value_list"`
	Info struct {
		InputType int `json:"input_type"`
	} `json:"attribute_info"`
}

// GetAttributeTree returns the attributes of up to 20 categories.
func (c *Client) GetAttributeTree(ctx context.Context, app App, token string, shopID int64, categoryIDs []int64) (map[int64][]Attribute, error) {
	var out struct {
		Response struct {
			List []struct {
				CategoryID int64       `json:"category_id"`
				Tree       []Attribute `json:"attribute_tree"`
			} `json:"list"`
		} `json:"response"`
	}
	q := url.Values{"category_id_list": {ids(categoryIDs)}}
	if _, err := c.Do(ctx, app, shopGet(PathGetAttributeTree, token, shopID, q), &out); err != nil {
		return nil, err
	}
	m := map[int64][]Attribute{}
	for _, l := range out.Response.List {
		m[l.CategoryID] = l.Tree
	}
	return m, nil
}

// Brand is one entry of get_brand_list.
type Brand struct {
	ID          int64  `json:"brand_id"`
	Name        string `json:"original_brand_name"`
	DisplayName string `json:"display_brand_name"`
}

// BrandPage is one page of get_brand_list.
type BrandPage struct {
	Brands      []Brand `json:"brand_list"`
	HasNextPage bool    `json:"has_next_page"`
	NextOffset  int64   `json:"next_offset"`
	IsMandatory bool    `json:"is_mandatory"`
}

// GetBrandList reads one page of normal brands for a category.
func (c *Client) GetBrandList(ctx context.Context, app App, token string, shopID, categoryID, offset int64) (BrandPage, error) {
	var out struct {
		Response BrandPage `json:"response"`
	}
	q := url.Values{"category_id": {strconv.FormatInt(categoryID, 10)}, "offset": {strconv.FormatInt(offset, 10)},
		"page_size": {"100"}, "status": {"1"}}
	_, err := c.Do(ctx, app, shopGet(PathGetBrandList, token, shopID, q), &out)
	return out.Response, err
}

// Range is a min/max limit.
type Range struct {
	Min float64 `json:"min_limit"`
	Max float64 `json:"max_limit"`
}

// ItemLimit is the part of get_item_limit we enforce.
type ItemLimit struct {
	Price      Range `json:"price_limit"`
	Stock      Range `json:"stock_limit"`
	NameLength Range `json:"item_name_length_limit"`
	ImageCount Range `json:"item_image_count_limit"`
	DescLength Range `json:"item_description_length_limit"`
	OptionName Range `json:"tier_variation_option_length_limit"`
	TierName   Range `json:"tier_variation_name_length_limit"`
}

// GetItemLimit reads the product limits of a category.
func (c *Client) GetItemLimit(ctx context.Context, app App, token string, shopID, categoryID int64) (ItemLimit, error) {
	var out struct {
		Response ItemLimit `json:"response"`
	}
	_, err := c.Do(ctx, app, shopGet(PathGetItemLimit, token, shopID, url.Values{"category_id": {strconv.FormatInt(categoryID, 10)}}), &out)
	return out.Response, err
}

// StdVariation is one standardised variation of get_variation_tree.
type StdVariation struct {
	ID     int64  `json:"variation_id"`
	Name   string `json:"variation_name"`
	Groups []struct {
		ID      int64  `json:"variation_group_id"`
		Name    string `json:"variation_group_name"`
		Options []struct {
			ID   int64  `json:"variation_option_id"`
			Name string `json:"variation_option_name"`
		} `json:"variation_option_list"`
	} `json:"variation_group_list"`
}

// GetVariationTree reads the standardised variations of a leaf category.
// Note: this response uses "data", not "response".
func (c *Client) GetVariationTree(ctx context.Context, app App, token string, shopID, categoryID int64) ([]StdVariation, error) {
	var out struct {
		Data struct {
			List []StdVariation `json:"standardise_variation_list"`
		} `json:"data"`
	}
	_, err := c.Do(ctx, app, shopGet(PathGetVariationTree, token, shopID, url.Values{"category_id": {strconv.FormatInt(categoryID, 10)}}), &out)
	return out.Data.List, err
}

// UploadedImage is the result of upload_image.
type UploadedImage struct {
	ImageID  string
	ImageURL string
}

// UploadImage uploads one image (multipart/form-data, field "image",
// scene "normal" or "desc"). Uploads are not idempotent but harmless
// (an orphan image at worst), so transient failures are retried.
func (c *Client) UploadImage(ctx context.Context, app App, token string, shopID int64, data []byte, contentType, scene string) (UploadedImage, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	ext := "jpg"
	if contentType == "image/png" {
		ext = "png"
	}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename="image.%s"`, ext))
	h.Set("Content-Type", contentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return UploadedImage{}, err
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		return UploadedImage{}, err
	}
	if err := mw.WriteField("scene", scene); err != nil {
		return UploadedImage{}, err
	}
	if err := mw.Close(); err != nil {
		return UploadedImage{}, err
	}
	var out struct {
		Response struct {
			ImageInfo struct {
				ImageID string `json:"image_id"`
				URLs    []struct {
					Region string `json:"image_url_region"`
					URL    string `json:"image_url"`
				} `json:"image_url_list"`
			} `json:"image_info"`
		} `json:"response"`
	}
	_, err = c.Do(ctx, app, Request{Method: "POST", Path: PathUploadImage, Kind: signer.Shop, AccessToken: token, TargetID: shopID,
		RawBody: body.Bytes(), ContentType: mw.FormDataContentType(), Idempotent: true}, &out)
	if err != nil {
		return UploadedImage{}, err
	}
	if out.Response.ImageInfo.ImageID == "" {
		return UploadedImage{}, &Error{Kind: KindUnknown, Code: "invalid_response", Message: "Upload returned no image_id.", Path: PathUploadImage}
	}
	img := UploadedImage{ImageID: out.Response.ImageInfo.ImageID}
	if len(out.Response.ImageInfo.URLs) > 0 {
		img.ImageURL = out.Response.ImageInfo.URLs[0].URL
	}
	return img, nil
}

// AddItem creates an item. NOT idempotent: an ambiguous failure must be
// reconciled with SearchItemBySKU before any retry.
func (c *Client) AddItem(ctx context.Context, app App, token string, shopID int64, item map[string]any) (int64, Meta, error) {
	var out struct {
		Response struct {
			ItemID int64 `json:"item_id"`
		} `json:"response"`
	}
	meta, err := c.Do(ctx, app, Request{Method: "POST", Path: PathAddItem, Kind: signer.Shop, AccessToken: token, TargetID: shopID, Body: item}, &out)
	if err == nil && out.Response.ItemID == 0 {
		err = &Error{Kind: KindAmbiguous, Code: "invalid_response", Message: "add_item returned no item_id.", RequestID: meta.RequestID, Path: PathAddItem}
	}
	return out.Response.ItemID, meta, err
}

// Model is a created or listed model.
type Model struct {
	ModelID   int64  `json:"model_id"`
	TierIndex []int  `json:"tier_index"`
	ModelSKU  string `json:"model_sku"`
}

// InitTierVariation sets the variation structure and creates the models.
// Fails if a structure already exists, so it is reconciled with
// GetModelList before a retry.
func (c *Client) InitTierVariation(ctx context.Context, app App, token string, shopID int64, body map[string]any) ([]Model, Meta, error) {
	var out struct {
		Response struct {
			Model []Model `json:"model"`
		} `json:"response"`
	}
	meta, err := c.Do(ctx, app, Request{Method: "POST", Path: PathInitTierVariation, Kind: signer.Shop, AccessToken: token, TargetID: shopID, Body: body}, &out)
	return out.Response.Model, meta, err
}

// SearchItemBySKU returns the IDs of items with this item_sku.
func (c *Client) SearchItemBySKU(ctx context.Context, app App, token string, shopID int64, sku string) ([]int64, error) {
	var out struct {
		Response struct {
			IDs []int64 `json:"item_id_list"`
		} `json:"response"`
	}
	q := url.Values{"item_sku": {sku}, "page_size": {"10"}}
	_, err := c.Do(ctx, app, shopGet(PathSearchItem, token, shopID, q), &out)
	return out.Response.IDs, err
}

// ItemBase is the part of get_item_base_info used for verification.
type ItemBase struct {
	ItemID     int64  `json:"item_id"`
	CategoryID int64  `json:"category_id"`
	Name       string `json:"item_name"`
	SKU        string `json:"item_sku"`
	Status     string `json:"item_status"`
	HasModel   bool   `json:"has_model"`
}

// GetItemBaseInfo reads up to 50 items.
func (c *Client) GetItemBaseInfo(ctx context.Context, app App, token string, shopID int64, itemIDs []int64) ([]ItemBase, error) {
	var out struct {
		Response struct {
			List []ItemBase `json:"item_list"`
		} `json:"response"`
	}
	_, err := c.Do(ctx, app, shopGet(PathGetItemBaseInfo, token, shopID, url.Values{"item_id_list": {ids(itemIDs)}}), &out)
	return out.Response.List, err
}

// GetModelList reads an item's models.
func (c *Client) GetModelList(ctx context.Context, app App, token string, shopID, itemID int64) ([]Model, error) {
	var out struct {
		Response struct {
			Model []Model `json:"model"`
		} `json:"response"`
	}
	_, err := c.Do(ctx, app, shopGet(PathGetModelList, token, shopID, url.Values{"item_id": {strconv.FormatInt(itemID, 10)}}), &out)
	return out.Response.Model, err
}
