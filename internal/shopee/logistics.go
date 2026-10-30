package shopee

import (
	"context"
	"encoding/json"

	"shopee-automation/internal/shopee/signer"
)

// Paths of the logistics and profile write endpoints (v2 API reference,
// read 2026-10-30).
const (
	PathGetChannelList   = "/api/v2/logistics/get_channel_list"
	PathGetAddressList   = "/api/v2/logistics/get_address_list"
	PathUpdateProfile    = "/api/v2/shop/update_profile"
	PathUpdateChannel    = "/api/v2/logistics/update_channel"
	PathUpdateAddress    = "/api/v2/logistics/update_address"
	PathSetAddressConfig = "/api/v2/logistics/set_address_config"
)

// Channel is one entry of get_channel_list.
type Channel struct {
	ID          int64  `json:"logistics_channel_id"`
	Name        string `json:"logistics_channel_name"`
	CODEnabled  bool   `json:"cod_enabled"` // channel supports COD
	Enabled     bool   `json:"enabled"`
	ForceEnable bool   `json:"force_enable"`
	FeeType     string `json:"fee_type"`
	MaskID      int64  `json:"mask_channel_id"`
}

// GetChannelList reads the shop's logistics channels.
func (c *Client) GetChannelList(ctx context.Context, app App, token string, shopID int64) ([]Channel, error) {
	var out struct {
		Response struct {
			List []Channel `json:"logistics_channel_list"`
		} `json:"response"`
	}
	_, err := c.Do(ctx, app, Request{Method: "GET", Path: PathGetChannelList, Kind: signer.Shop, AccessToken: token,
		TargetID: shopID, Idempotent: true}, &out)
	if out.Response.List == nil {
		out.Response.List = []Channel{}
	}
	return out.Response.List, err
}

// Address is one entry of get_address_list. The read API does not return
// the recipient name or phone.
type Address struct {
	ID          int64    `json:"address_id"`
	Region      string   `json:"region"`
	State       string   `json:"state"`
	City        string   `json:"city"`
	District    string   `json:"district"`
	Town        string   `json:"town"`
	Address     string   `json:"address"`
	Zipcode     string   `json:"zipcode"`
	AddressType []string `json:"address_type"`
}

// Address type flags. get_address_list documents PICK_UP_ADDRESS while
// set_address_config documents PICKUP_ADDRESS; NormalizeAddressType maps
// the read form to the write form.
const (
	AddressDefault = "DEFAULT_ADDRESS"
	AddressPickup  = "PICKUP_ADDRESS"
	AddressReturn  = "RETURN_ADDRESS"
)

// NormalizeAddressType returns the set_address_config spelling.
func NormalizeAddressType(t string) string {
	if t == "PICK_UP_ADDRESS" {
		return AddressPickup
	}
	return t
}

// AddressList is the response of get_address_list.
type AddressList struct {
	ShowPickupAddress bool      `json:"show_pickup_address"`
	Addresses         []Address `json:"address_list"`
}

// GetAddressList reads the shop's addresses.
func (c *Client) GetAddressList(ctx context.Context, app App, token string, shopID int64) (AddressList, error) {
	var out struct {
		Response AddressList `json:"response"`
	}
	_, err := c.Do(ctx, app, Request{Method: "GET", Path: PathGetAddressList, Kind: signer.Shop, AccessToken: token,
		TargetID: shopID, Idempotent: true}, &out)
	if out.Response.Addresses == nil {
		out.Response.Addresses = []Address{}
	}
	for i := range out.Response.Addresses {
		for j, t := range out.Response.Addresses[i].AddressType {
			out.Response.Addresses[i].AddressType[j] = NormalizeAddressType(t)
		}
	}
	return out.Response, err
}

// ProfileUpdate holds the fields update_profile accepts. Nil fields are not sent.
type ProfileUpdate struct {
	ShopName    *string `json:"shop_name,omitempty"`
	ShopLogo    *string `json:"shop_logo,omitempty"`
	Description *string `json:"description,omitempty"`
}

// UpdateProfile changes the shop name, logo URL or description. Setting a
// value is repeatable, so the call is retried on transient errors.
func (c *Client) UpdateProfile(ctx context.Context, app App, token string, shopID int64, u ProfileUpdate) (Meta, error) {
	return c.Do(ctx, app, Request{Method: "POST", Path: PathUpdateProfile, Kind: signer.Shop, AccessToken: token,
		TargetID: shopID, Body: u, Idempotent: true}, nil)
}

// ChannelUpdate holds the fields of update_channel this project uses.
type ChannelUpdate struct {
	ID         int64 `json:"logistics_channel_id"`
	Enabled    *bool `json:"enabled,omitempty"`
	CODEnabled *bool `json:"cod_enabled,omitempty"`
}

// UpdateChannel enables or disables a channel and COD.
func (c *Client) UpdateChannel(ctx context.Context, app App, token string, shopID int64, u ChannelUpdate) (Meta, error) {
	return c.Do(ctx, app, Request{Method: "POST", Path: PathUpdateChannel, Kind: signer.Shop, AccessToken: token,
		TargetID: shopID, Body: u, Idempotent: true}, nil)
}

// AddressUpdate holds the fields of update_address. The region cannot be
// changed (documented); it is never sent.
type AddressUpdate struct {
	ID       int64   `json:"address_id"`
	State    *string `json:"state,omitempty"`
	City     *string `json:"city,omitempty"`
	District *string `json:"district,omitempty"`
	Town     *string `json:"town,omitempty"`
	Address  *string `json:"address,omitempty"`
	Zipcode  *string `json:"zipcode,omitempty"`
	Name     *string `json:"name,omitempty"`
	Phone    *string `json:"phone,omitempty"`
}

// UpdateAddress edits an existing address.
func (c *Client) UpdateAddress(ctx context.Context, app App, token string, shopID int64, u AddressUpdate) (Meta, error) {
	return c.Do(ctx, app, Request{Method: "POST", Path: PathUpdateAddress, Kind: signer.Shop, AccessToken: token,
		TargetID: shopID, Body: u, Idempotent: true}, nil)
}

// SetAddressConfig flags an address as default, pickup and/or return address.
func (c *Client) SetAddressConfig(ctx context.Context, app App, token string, shopID int64, addressID int64, types []string) (Meta, error) {
	body := map[string]any{"address_type_config": map[string]any{"address_id": addressID, "address_type": types}}
	return c.Do(ctx, app, Request{Method: "POST", Path: PathSetAddressConfig, Kind: signer.Shop, AccessToken: token,
		TargetID: shopID, Body: body, Idempotent: true}, nil)
}

// RawJSON marshals v for snapshots.
func RawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
