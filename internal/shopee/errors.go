package shopee

import (
	"errors"
	"fmt"
)

// Kind classifies a Shopee failure by what the caller should do about it.
type Kind int

const (
	// KindUnknown is an error we have no rule for. Not retried.
	KindUnknown Kind = iota
	// KindTransient is a temporary Shopee or network failure. Reads retry.
	KindTransient
	// KindRateLimited means Shopee throttled the call. Safe to retry later.
	KindRateLimited
	// KindAuth means the token or code was rejected for this call.
	KindAuth
	// KindAuthorizationLost means the shop's authorization is gone (expired,
	// revoked, banned, refresh token dead). The seller must reconnect.
	KindAuthorizationLost
	// KindAppConfig means our partner app is misconfigured (wrong key, IP
	// whitelist, app type). An operator must fix it; sellers cannot.
	KindAppConfig
	// KindPermission means the app or shop is not allowed to call this API.
	KindPermission
	// KindValidation means Shopee rejected the request parameters.
	KindValidation
	// KindNotFound means the requested object does not exist.
	KindNotFound
	// KindAmbiguous means a write was sent but no answer arrived. It may or
	// may not have been applied; the caller must reconcile, not retry blindly.
	KindAmbiguous
)

func (k Kind) String() string {
	return [...]string{"unknown", "transient", "rate_limited", "auth", "authorization_lost",
		"app_config", "permission", "validation", "not_found", "ambiguous"}[k]
}

// Error is a normalized Shopee failure. Message comes from Shopee and is safe
// to show to members who manage shops; it never contains our secrets.
type Error struct {
	Kind       Kind
	Code       string // Shopee "error" field, or a local code like "network"
	Message    string
	RequestID  string
	HTTPStatus int
	Path       string // API path, without query
	cause      error
}

func (e *Error) Error() string {
	s := fmt.Sprintf("shopee %s: %s (%s)", e.Path, e.Code, e.Kind)
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.RequestID != "" {
		s += " request_id=" + e.RequestID
	}
	return s
}

// Unwrap returns the transport cause, already stripped of URLs.
func (e *Error) Unwrap() error { return e.cause }

// AsError extracts a *Error.
func AsError(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// IsKind reports whether err is a Shopee error of kind k.
func IsKind(err error, k Kind) bool {
	e, ok := AsError(err)
	return ok && e.Kind == k
}

// codeKinds maps documented Shopee error codes to kinds. Source: error lists
// of the v2 API reference pages for auth/token/get, auth/access_token/get,
// shop/get_shop_info and the common error list (read 2026-10-30). Add codes
// only when they are documented or observed in sandbox.
var codeKinds = map[string]Kind{
	"error_network":     KindTransient,
	"error_server":      KindTransient,
	"error_system_busy": KindTransient,
	"error_inner":       KindTransient,

	"error_rate_limit": KindRateLimited,
	"error_limit":      KindRateLimited, // daily quota; resets 00:00 UTC+8

	"error_auth":             KindAuth, // broad: also used for "System error" and stock rules
	"invalid_acceess_token":  KindAuth, // sic, as documented
	"invalid_code":           KindAuth,
	"invalid_shop_id":        KindAuth,
	"invalid_main_acount_id": KindAuth, // sic, as documented

	"shop_access_expired":          KindAuthorizationLost,
	"refresh_token_expired":        KindAuthorizationLost,
	"error_shop_refresh_token":     KindAuthorizationLost,
	"error_merchant_refresh_token": KindAuthorizationLost,
	"merchant_access_expired":      KindAuthorizationLost,
	"shop_no_linked":               KindAuthorizationLost,
	"partner_shop_no_link":         KindAuthorizationLost,
	"merchant_no_linked":           KindAuthorizationLost,
	"shop_banned":                  KindAuthorizationLost,

	"error_sign":                KindAppConfig,
	"invalid_partner_id":        KindAppConfig,
	"error_partner_key_expired": KindAppConfig,
	"source_ip_undeclared":      KindAppConfig,
	"error_api_permission":      KindAppConfig,
	"error_api_call_restricted": KindAppConfig,
	"api_suspended":             KindAppConfig,

	"error_permission":           KindPermission,
	"error_ashop_api_permission": KindPermission,
	"error_kyc_auth":             KindPermission,
	"error_perm_non_admin":       KindPermission,

	"error_param":      KindValidation,
	"error_data_check": KindValidation,
	"error_data":       KindValidation,
	"error_shop":       KindValidation,

	"error_not_found":               KindNotFound,
	"error_not_exists":              KindNotFound,
	"error_item_not_found":          KindNotFound,
	"error_shop_not_found":          KindNotFound,
	"error_auth_shop_not_found":     KindNotFound,
	"error_param_shop_id_not_found": KindNotFound,
}

func classify(code string, status int) Kind {
	if k, ok := codeKinds[code]; ok {
		return k
	}
	switch {
	case status == 429:
		return KindRateLimited
	case status >= 500:
		return KindTransient
	}
	return KindUnknown
}
