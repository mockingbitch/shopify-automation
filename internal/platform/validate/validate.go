// Package validate provides small, explicit input validation helpers that
// collect field errors into an apperr validation error.
package validate

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"shopee-automation/internal/platform/apperr"
)

// Errors collects field errors.
type Errors struct{ fields []apperr.FieldError }

// Add records an error for field.
func (v *Errors) Add(field, code, msg string) {
	v.fields = append(v.fields, apperr.FieldError{Field: field, Code: code, Message: msg})
}

// Err returns nil when there are no errors, otherwise a validation error.
func (v *Errors) Err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return apperr.Invalid(v.fields...)
}

// Length checks the rune length of s.
func (v *Errors) Length(field, s string, min, max int) bool {
	n := utf8.RuneCountInString(s)
	switch {
	case n < min && min == 1:
		v.Add(field, "required", "Bạn chưa nhập trường này.")
	case n < min:
		v.Add(field, "too_short", "Cần ít nhất "+itoa(min)+" ký tự.")
	case n > max:
		v.Add(field, "too_long", "Tối đa "+itoa(max)+" ký tự.")
	default:
		return true
	}
	return false
}

// NormalizeEmail lowercases and trims an email address.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Email checks that s is a bare address (no display name), at most 254 chars.
func (v *Errors) Email(field, s string) bool {
	if s == "" {
		v.Add(field, "required", "Bạn chưa nhập trường này.")
		return false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || len(s) > 254 || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		v.Add(field, "invalid_format", "Hãy nhập một địa chỉ email hợp lệ.")
		return false
	}
	return true
}

var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$`)

var reservedSlugs = map[string]struct{}{
	"admin": {}, "api": {}, "app": {}, "auth": {}, "login": {}, "logout": {}, "settings": {},
	"support": {}, "help": {}, "www": {}, "static": {}, "assets": {}, "oauth": {}, "webhooks": {},
	"internal": {}, "ext": {}, "invitations": {}, "orgs": {}, "me": {},
}

// Slug checks an organization slug.
func (v *Errors) Slug(field, s string) bool {
	if !slugRe.MatchString(s) {
		v.Add(field, "invalid_format", "Dùng 3–40 ký tự gồm chữ thường không dấu, chữ số hoặc dấu gạch ngang; bắt đầu và kết thúc bằng chữ hoặc số.")
		return false
	}
	if _, ok := reservedSlugs[s]; ok {
		v.Add(field, "reserved", "Tên này đã được hệ thống dành riêng. Hãy chọn tên khác.")
		return false
	}
	return true
}

// commonPasswords is a small denylist. A breached-password check (k-anonymity
// range API) is planned for Phase 8; see docs/api-design.md.
var commonPasswords = map[string]struct{}{
	"password1234": {}, "123456789012": {}, "qwertyuiopas": {}, "passwordpassword": {},
	"iloveyou1234": {}, "adminadmin12": {}, "shopee123456": {}, "matkhau12345": {},
}

// Password checks length and a denylist. Returns true when acceptable.
func (v *Errors) Password(field, pw string) bool {
	n := utf8.RuneCountInString(pw)
	if n < 12 {
		v.Add(field, "too_short", "Mật khẩu cần ít nhất 12 ký tự.")
		return false
	}
	if n > 128 {
		v.Add(field, "too_long", "Mật khẩu tối đa 128 ký tự.")
		return false
	}
	if _, bad := commonPasswords[strings.ToLower(pw)]; bad {
		v.Add(field, "too_common", "Mật khẩu này quá phổ biến. Hãy chọn mật khẩu khó đoán hơn.")
		return false
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
