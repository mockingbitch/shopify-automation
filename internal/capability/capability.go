// Package capability resolves which shop operations are available and how
// they run (Open API, browser extension, or a manual task), with a reason the
// user can read. Resolution is a pure function of the registry, overrides and
// the shop's context, so it is easy to test and explain.
package capability

import (
	"slices"

	"github.com/google/uuid"
)

// Mode is how an operation is executed.
type Mode string

const (
	ModeAPI         Mode = "API"
	ModeBrowser     Mode = "BROWSER"
	ModeManual      Mode = "MANUAL"
	ModeUnsupported Mode = "UNSUPPORTED"
)

// Verification levels (see docs/shopee-integration.md §1).
const (
	DocVerified     = "DOC_VERIFIED"
	SandboxVerified = "SANDBOX_VERIFIED"
)

// Reason codes. Stable strings: the dashboard maps them to text.
const (
	ReasonOK                   = ""
	ReasonUnsupported          = "not_supported"
	ReasonDisabled             = "disabled_by_platform"
	ReasonRegion               = "region_not_supported"
	ReasonAppType              = "app_type_not_allowed"
	ReasonShopNotActive        = "shop_not_active"
	ReasonNotSandboxVerified   = "not_sandbox_verified"
	ReasonExtensionUnavailable = "browser_extension_unavailable"
)

// Definition is one registry entry.
type Definition struct {
	Key              string
	Title            string
	DefaultMode      Mode
	FallbackMode     Mode     // "" if none
	RequiredAppTypes []string // nil: any
	SupportedRegions []string // nil: any
	RequiresApproval bool
	Sensitive        bool
	Enabled          bool
	Verification     string
}

// Override changes a definition for one app, one region, or both. Nil
// fields leave the value unchanged.
type Override struct {
	CapabilityKey string
	AppID         *uuid.UUID
	Region        *string
	Mode          *Mode
	Enabled       *bool
	Reason        string
}

// Context describes the shop being resolved.
type Context struct {
	AppID            uuid.UUID
	AppType          string
	AppEnvironment   string // sandbox | production
	Region           string
	ConnectionStatus string
	// ExtensionAvailable is false until the browser extension ships (Phase 6).
	ExtensionAvailable bool
}

// Resolved is the outcome for one capability.
type Resolved struct {
	Key              string `json:"key"`
	Title            string `json:"title"`
	Mode             Mode   `json:"mode"`
	Enabled          bool   `json:"enabled"`
	Reason           string `json:"reason"`
	RequiresApproval bool   `json:"requires_approval"`
	Sensitive        bool   `json:"sensitive"`
	Verification     string `json:"verification"`
}

// Resolve evaluates every definition for ctx.
func Resolve(defs []Definition, overrides []Override, ctx Context) []Resolved {
	out := make([]Resolved, 0, len(defs))
	for _, d := range defs {
		out = append(out, resolveOne(d, overridesFor(d.Key, overrides, ctx), ctx))
	}
	return out
}

// overridesFor returns matching overrides from least to most specific.
func overridesFor(key string, all []Override, ctx Context) []Override {
	var match []Override
	for _, o := range all {
		if o.CapabilityKey != key {
			continue
		}
		if o.AppID != nil && *o.AppID != ctx.AppID {
			continue
		}
		if o.Region != nil && *o.Region != ctx.Region {
			continue
		}
		match = append(match, o)
	}
	specificity := func(o Override) int {
		n := 0
		if o.AppID != nil {
			n++
		}
		if o.Region != nil {
			n += 2
		}
		return n
	}
	slices.SortStableFunc(match, func(a, b Override) int { return specificity(a) - specificity(b) })
	return match
}

func resolveOne(d Definition, ovs []Override, ctx Context) Resolved {
	r := Resolved{Key: d.Key, Title: d.Title, Mode: d.DefaultMode, Enabled: d.Enabled,
		RequiresApproval: d.RequiresApproval, Sensitive: d.Sensitive, Verification: d.Verification}
	overrideReason := ""
	for _, o := range ovs {
		if o.Mode != nil {
			r.Mode = *o.Mode
		}
		if o.Enabled != nil {
			r.Enabled = *o.Enabled
		}
		overrideReason = o.Reason
	}

	disable := func(reason string) Resolved {
		r.Enabled = false
		r.Reason = reason
		return r
	}
	switch {
	case r.Mode == ModeUnsupported:
		return disable(ReasonUnsupported)
	case !r.Enabled:
		if overrideReason != "" {
			return disable(overrideReason)
		}
		return disable(ReasonDisabled)
	case d.SupportedRegions != nil && !slices.Contains(d.SupportedRegions, ctx.Region):
		return disable(ReasonRegion)
	case d.RequiredAppTypes != nil && !slices.Contains(d.RequiredAppTypes, ctx.AppType):
		return disable(ReasonAppType)
	case ctx.ConnectionStatus != "ACTIVE":
		return disable(ReasonShopNotActive)
	}

	switch r.Mode {
	case ModeAPI:
		// Production only runs API operations that were exercised in sandbox.
		if ctx.AppEnvironment == "production" && d.Verification != SandboxVerified {
			return disable(ReasonNotSandboxVerified)
		}
	case ModeBrowser:
		if !ctx.ExtensionAvailable {
			if d.FallbackMode == ModeManual {
				r.Mode = ModeManual
				r.Reason = ReasonExtensionUnavailable
				return r
			}
			return disable(ReasonExtensionUnavailable)
		}
	}
	return r
}
