package shopee

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"shopee-automation/internal/shopee/signer"
)

// Field names and paths below come from the v2 API reference
// (open.shopee.com/documents/v2/<api>, read 2026-10-30).

// Paths of the endpoints this package wraps.
const (
	PathGetAccessToken     = "/api/v2/auth/token/get"        // #nosec G101 -- API path, not a credential
	PathRefreshAccessToken = "/api/v2/auth/access_token/get" // #nosec G101 -- API path, not a credential
	PathGetShopInfo        = "/api/v2/shop/get_shop_info"
	PathGetProfile         = "/api/v2/shop/get_profile"
)

// AccessTokenLifetime is the documented access token lifetime (4 hours).
const AccessTokenLifetime = 4 * time.Hour

// RefreshTokenLifetime is the documented refresh token lifetime (30 days).
const RefreshTokenLifetime = 30 * 24 * time.Hour

// Tokens is the result of token/get or access_token/get.
type Tokens struct {
	AccessToken    string  `json:"access_token"`
	RefreshToken   string  `json:"refresh_token"`
	ExpireIn       int64   `json:"expire_in"`
	ShopIDList     []int64 `json:"shop_id_list"`
	MerchantIDList []int64 `json:"merchant_id_list"`
	ShopID         int64   `json:"shop_id"`
	RequestID      string  `json:"request_id"`
}

// AccessTTL returns how long the access token is valid. The reference
// describes expire_in as seconds, but the token/get sample shows a Unix
// timestamp; both are handled, and the result is capped at the documented
// 4 hours so we never wait too long to refresh.
func (t Tokens) AccessTTL(now time.Time) time.Duration {
	d := time.Duration(t.ExpireIn) * time.Second
	if t.ExpireIn > 1_000_000_000 { // looks like a Unix timestamp
		d = time.Unix(t.ExpireIn, 0).Sub(now)
	}
	if d <= 0 || d > AccessTokenLifetime {
		d = AccessTokenLifetime
	}
	return d
}

// GetAccessToken exchanges an authorization code. Exactly one of shopID and
// mainAccountID must be set. The code is single-use, so this is never
// retried after the request may have reached Shopee.
func (c *Client) GetAccessToken(ctx context.Context, app App, code string, shopID, mainAccountID int64) (Tokens, error) {
	body := map[string]any{"code": code, "partner_id": app.PartnerID}
	if mainAccountID != 0 {
		body["main_account_id"] = mainAccountID
	} else {
		body["shop_id"] = shopID
	}
	var t Tokens
	_, err := c.Do(ctx, app, Request{Method: "POST", Path: PathGetAccessToken, Kind: signer.Public, Body: body}, &t)
	if err == nil && (t.AccessToken == "" || t.RefreshToken == "") {
		err = &Error{Kind: KindUnknown, Code: "invalid_response", Message: "Token response is missing tokens.", RequestID: t.RequestID, Path: PathGetAccessToken}
	}
	return t, err
}

// RefreshAccessToken trades a refresh token for a new pair. Refresh tokens
// are single-use: never retried after the request may have reached Shopee.
func (c *Client) RefreshAccessToken(ctx context.Context, app App, refreshToken string, shopID int64) (Tokens, error) {
	body := map[string]any{"refresh_token": refreshToken, "partner_id": app.PartnerID, "shop_id": shopID}
	var t Tokens
	_, err := c.Do(ctx, app, Request{Method: "POST", Path: PathRefreshAccessToken, Kind: signer.Public, Body: body}, &t)
	if err == nil && (t.AccessToken == "" || t.RefreshToken == "") {
		err = &Error{Kind: KindUnknown, Code: "invalid_response", Message: "Token response is missing tokens.", RequestID: t.RequestID, Path: PathRefreshAccessToken}
	}
	return t, err
}

// ShopInfo is the subset of get_shop_info we use. Raw keeps the full body.
type ShopInfo struct {
	ShopName       string          `json:"shop_name"`
	Region         string          `json:"region"`
	Status         string          `json:"status"` // BANNED, FROZEN, NORMAL
	IsCB           bool            `json:"is_cb"`
	IsSIP          bool            `json:"is_sip"`
	IsUpgradedCBSC bool            `json:"is_upgraded_cbsc"`
	MerchantID     *int64          `json:"merchant_id"`
	AuthTime       int64           `json:"auth_time"`
	ExpireTime     int64           `json:"expire_time"`
	RequestID      string          `json:"request_id"`
	Raw            json.RawMessage `json:"-"`
}

// GetShopInfo reads basic shop information.
func (c *Client) GetShopInfo(ctx context.Context, app App, accessToken string, shopID int64) (ShopInfo, error) {
	var raw json.RawMessage
	_, err := c.Do(ctx, app, Request{Method: "GET", Path: PathGetShopInfo, Kind: signer.Shop,
		AccessToken: accessToken, TargetID: shopID, Idempotent: true}, &raw)
	if err != nil {
		return ShopInfo{}, err
	}
	var s ShopInfo
	if err := json.Unmarshal(raw, &s); err != nil {
		return ShopInfo{}, &Error{Kind: KindUnknown, Code: "invalid_response", Path: PathGetShopInfo}
	}
	s.Raw = raw
	return s, nil
}

// Profile is the response of get_profile.
type Profile struct {
	ShopLogo    string          `json:"shop_logo"`
	Description string          `json:"description"`
	ShopName    string          `json:"shop_name"`
	Raw         json.RawMessage `json:"-"`
}

// GetProfile reads the shop logo, name and description.
func (c *Client) GetProfile(ctx context.Context, app App, accessToken string, shopID int64) (Profile, error) {
	var out struct {
		Response json.RawMessage `json:"response"`
	}
	_, err := c.Do(ctx, app, Request{Method: "GET", Path: PathGetProfile, Kind: signer.Shop,
		AccessToken: accessToken, TargetID: shopID, Idempotent: true}, &out)
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	if len(out.Response) > 0 {
		if err := json.Unmarshal(out.Response, &p); err != nil {
			return Profile{}, &Error{Kind: KindUnknown, Code: "invalid_response", Path: PathGetProfile}
		}
	}
	p.Raw = out.Response
	return p, nil
}

// AuthURL builds the seller authorization link (new format, supports state):
// <auth_host>/auth?partner_id&auth_type=seller&redirect_uri&response_type=code&state
// Source: developer guide 20 "Authorization and Authentication".
func AuthURL(authHost string, partnerID int64, redirectURI, state string) string {
	q := url.Values{}
	q.Set("partner_id", strconv.FormatInt(partnerID, 10))
	q.Set("auth_type", "seller")
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("state", state)
	return strings.TrimRight(authHost, "/") + "/auth?" + q.Encode()
}

// VerifyPush checks the Authorization header of a push notification:
// hex(HMAC-SHA256(partner_key, callback_url + "|" + raw_body)).
// Source: developer guide 18 "Push Mechanism".
func VerifyPush(partnerKey []byte, callbackURL string, body []byte, authorization string) bool {
	if authorization == "" {
		return false
	}
	want := signer.HMAC(partnerKey, callbackURL+"|"+string(body))
	return signer.Equal(strings.ToLower(strings.TrimSpace(authorization)), want)
}
