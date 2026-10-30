package validate

import (
	"strings"
	"testing"

	"shopee-automation/internal/platform/apperr"
)

func TestEmail(t *testing.T) {
	ok := []string{"a@b.co", "first.last+tag@example.vn"}
	bad := []string{"", "a", "a@b", "Name <a@b.co>", "a@b.co ", strings.Repeat("a", 250) + "@b.co"}
	for _, s := range ok {
		var v Errors
		if !v.Email("email", s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range bad {
		var v Errors
		if v.Email("email", s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestSlug(t *testing.T) {
	for s, want := range map[string]bool{
		"acme": true, "my-shop-1": true, "ab": false, "-ab": false, "ab-": false,
		"ABC": false, "admin": false, strings.Repeat("a", 41): false,
	} {
		var v Errors
		if got := v.Slug("slug", s); got != want {
			t.Errorf("Slug(%q)=%v want %v", s, got, want)
		}
	}
}

func TestPassword(t *testing.T) {
	var v Errors
	if v.Password("password", "short") {
		t.Error("short password accepted")
	}
	if v.Password("password", "Password1234") {
		t.Error("common password accepted (case-insensitive)")
	}
	if !v.Password("password", "correct horse battery") {
		t.Error("good password rejected")
	}
	err := v.Err()
	e, ok := apperr.As(err)
	if !ok || e.Kind != apperr.KindInvalid || len(e.Fields) != 2 {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestLength(t *testing.T) {
	var v Errors
	v.Length("name", "", 1, 10)
	v.Length("name", "Tiếng Việt", 1, 10) // 10 runes, more bytes
	v.Length("name", "toolongvalue", 1, 10)
	e, _ := apperr.As(v.Err())
	if len(e.Fields) != 2 || e.Fields[0].Code != "required" || e.Fields[1].Code != "too_long" {
		t.Fatalf("fields: %+v", e.Fields)
	}
}
