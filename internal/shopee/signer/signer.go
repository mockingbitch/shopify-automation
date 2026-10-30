// Package signer computes Shopee Open Platform v2 request signatures.
//
// Source: https://open.shopee.com/developer-guide/16 (read 2026-10-30).
//
//	sign = lowercase hex(HMAC-SHA256(partner_key, base_string))
//	Public:   partner_id + api_path + timestamp
//	Shop:     partner_id + api_path + timestamp + access_token + shop_id
//	Merchant: partner_id + api_path + timestamp + access_token + merchant_id
package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// Kind is the Shopee API type, which decides the base string.
type Kind int

const (
	// Public APIs (auth, public.*) sign partner_id + path + timestamp.
	Public Kind = iota
	// Shop APIs add access_token + shop_id.
	Shop
	// Merchant APIs add access_token + merchant_id.
	Merchant
)

// BaseString returns the string that is signed.
func BaseString(partnerID int64, path string, ts int64, kind Kind, accessToken string, targetID int64) string {
	s := strconv.FormatInt(partnerID, 10) + path + strconv.FormatInt(ts, 10)
	if kind == Shop || kind == Merchant {
		s += accessToken + strconv.FormatInt(targetID, 10)
	}
	return s
}

// Sign returns the request signature.
func Sign(partnerKey []byte, partnerID int64, path string, ts int64, kind Kind, accessToken string, targetID int64) string {
	return HMAC(partnerKey, BaseString(partnerID, path, ts, kind, accessToken, targetID))
}

// HMAC returns lowercase hex HMAC-SHA256 of msg. Push notifications use it
// over "callback_url|body".
func HMAC(key []byte, msg string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// Equal compares two hex signatures in constant time.
func Equal(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }
