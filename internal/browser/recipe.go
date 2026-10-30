// Package browser runs operations without an Open API through the browser
// extension: device pairing, signed recipes, and the task queue the
// extension works from (docs/browser-extension.md §11).
package browser

import (
	"bytes"
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"shopee-automation/internal/platform/apperr"
)

//go:embed recipe.schema.json
var recipeSchemaJSON []byte

var recipeSchema = func() *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(recipeSchemaJSON))
	if err != nil {
		panic(err)
	}
	c := jsonschema.NewCompiler()
	const url = "https://shopee-automation.local/schemas/recipe.json"
	if err := c.AddResource(url, doc); err != nil {
		panic(err)
	}
	s, err := c.Compile(url)
	if err != nil {
		panic(err)
	}
	return s
}()

// Recipe is the part of a recipe the server needs.
type Recipe struct {
	Capability     string                       `json:"capability"`
	Version        int                          `json:"version"`
	Market         string                       `json:"market"`
	StartURL       string                       `json:"start_url"`
	AllowedOrigins []string                     `json:"allowed_origins"`
	Inputs         map[string]RecipeInput       `json:"inputs"`
	Steps          []map[string]json.RawMessage `json:"steps"`
	Verify         []map[string]json.RawMessage `json:"verify"`
}

// RecipeInput declares one input. Inputs are required unless
// "required": false (the extension applies the same default).
type RecipeInput struct {
	Type     string `json:"type"`
	Optional *bool  `json:"required"`
}

// IsRequired reports whether the input must be present.
func (i RecipeInput) IsRequired() bool { return i.Optional == nil || *i.Optional }

var (
	templateRe = regexp.MustCompile(`\{\{\s*([^}]*?)\s*\}\}`)
	writeSteps = map[string]bool{"click": true, "fill": true, "select": true, "upload": true}
)

// ValidateRecipe checks a recipe against the schema and the rules the
// schema cannot express: the shop check comes before any write, templates
// refer to declared inputs, and the start URL is on an allowed origin.
func ValidateRecipe(raw []byte) (Recipe, []apperr.FieldError) {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Recipe{}, []apperr.FieldError{{Field: "recipe", Code: "invalid_json", Message: "The recipe must be a JSON object."}}
	}
	if err := recipeSchema.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if !errors.As(err, &ve) {
			return Recipe{}, []apperr.FieldError{{Field: "recipe", Code: "invalid", Message: "The recipe is invalid."}}
		}
		return Recipe{}, flatten(ve.BasicOutput())
	}
	var r Recipe
	if err := json.Unmarshal(raw, &r); err != nil {
		return Recipe{}, []apperr.FieldError{{Field: "recipe", Code: "invalid", Message: err.Error()}}
	}
	var errs []apperr.FieldError
	bad := func(field, code, msg string) {
		errs = append(errs, apperr.FieldError{Field: field, Code: code, Message: msg})
	}

	origin := func(u string) string {
		rest := strings.TrimPrefix(u, "https://")
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			rest = rest[:i]
		}
		return "https://" + rest
	}
	allowed := map[string]bool{}
	for _, o := range r.AllowedOrigins {
		allowed[o] = true
	}
	if !allowed[origin(r.StartURL)] {
		bad("recipe.start_url", "origin_not_allowed", "start_url must be on one of allowed_origins.")
	}

	checkTemplates := func(field, s string, wantType string) {
		for _, m := range templateRe.FindAllStringSubmatch(s, -1) {
			ref := m[1]
			switch {
			case ref == "task.shopee_shop_id" && wantType == "text":
			case strings.HasPrefix(ref, "input."):
				in, ok := r.Inputs[strings.TrimPrefix(ref, "input.")]
				if !ok {
					bad(field, "undeclared_input", fmt.Sprintf("%s is not declared in inputs.", ref))
				} else if in.Type != wantType {
					bad(field, "input_type", fmt.Sprintf("%s is a %s input; %s is needed here.", ref, in.Type, wantType))
				}
			default:
				bad(field, "bad_template", fmt.Sprintf("Unknown template {{%s}}; use {{task.shopee_shop_id}} or {{input.<name>}}.", ref))
			}
		}
	}
	// Templates are allowed only in goto.url, fill.value, select.value and
	// upload.files; anywhere else "{{" is an error (same rule as the
	// extension's validator).
	templateFields := map[string]map[string]bool{"goto": {"url": true}, "fill": {"value": true}, "select": {"value": true}, "upload": {"files": true}}
	var walk func(field string, v any, allowed bool)
	walk = func(field string, v any, allowed bool) {
		switch t := v.(type) {
		case string:
			if !allowed && strings.Contains(t, "{{") {
				bad(field, "template_not_allowed", "Templates are only allowed in goto.url, fill.value, select.value and upload.files.")
			}
		case map[string]any:
			for k, x := range t {
				walk(field+"."+k, x, false)
			}
		case []any:
			for i, x := range t {
				walk(fmt.Sprintf("%s.%d", field, i), x, false)
			}
		}
	}
	var whole map[string]any
	_ = json.Unmarshal(raw, &whole)
	for k, v := range whole {
		if k != "steps" && k != "verify" {
			walk("recipe."+k, v, false)
		}
	}
	checkSteps := func(list string, steps []map[string]json.RawMessage, requireShopCheck bool) {
		asserted := false
		for i, st := range steps {
			field := fmt.Sprintf("recipe.%s.%d", list, i)
			for action, body := range st {
				if action == "timeout_ms" {
					continue
				}
				if action == "assert_shop_id" {
					asserted = true
					var a struct {
						Pattern string `json:"pattern"`
					}
					_ = json.Unmarshal(body, &a)
					if re, err := regexp.Compile(a.Pattern); err != nil || re.NumSubexp() != 1 {
						bad(field, "bad_pattern", "pattern must be a valid regular expression with exactly one capture group.")
					}
				}
				if writeSteps[action] && requireShopCheck && !asserted {
					bad(field, "shop_check_missing", "assert_shop_id must come before the first click, fill, select or upload.")
				}
				if writeSteps[action] && list == "verify" {
					bad(field, "write_in_verify", "verify steps may only read the page.")
				}
				var obj map[string]any
				_ = json.Unmarshal(body, &obj)
				for k, x := range obj {
					walk(field+"."+action+"."+k, x, templateFields[action][k])
				}
				var v struct {
					URL   string `json:"url"`
					Value string `json:"value"`
					Files string `json:"files"`
				}
				_ = json.Unmarshal(body, &v)
				if action == "goto" {
					checkTemplates(field, v.URL, "text")
					onAllowed := false
					for o := range allowed {
						if strings.HasPrefix(v.URL, o+"/") {
							onAllowed = true
						}
					}
					if !onAllowed {
						bad(field, "origin_not_allowed", "goto must start with one of allowed_origins followed by a path (no template in the host).")
					}
				}
				checkTemplates(field, v.Value, "text")
				checkTemplates(field, v.Files, "files")
			}
		}
	}
	checkSteps("steps", r.Steps, true)
	checkSteps("verify", r.Verify, false)
	if len(errs) > 0 {
		return Recipe{}, errs
	}
	return r, nil
}

func flatten(out *jsonschema.OutputUnit) []apperr.FieldError {
	byLoc := map[string]string{}
	for _, u := range out.Errors {
		if u.Error == nil {
			continue
		}
		msg := u.Error.String()
		if strings.HasPrefix(msg, "validation failed") || strings.Contains(msg, "doesn't validate with") {
			continue
		}
		if _, seen := byLoc[u.InstanceLocation]; !seen {
			byLoc[u.InstanceLocation] = msg
		}
	}
	if len(byLoc) == 0 {
		return []apperr.FieldError{{Field: "recipe", Code: "invalid", Message: "The recipe is invalid."}}
	}
	locs := make([]string, 0, len(byLoc))
	for l := range byLoc {
		locs = append(locs, l)
	}
	sort.Strings(locs)
	out2 := make([]apperr.FieldError, 0, len(locs))
	for _, l := range locs {
		out2 = append(out2, apperr.FieldError{Field: "recipe" + strings.ReplaceAll(l, "/", "."), Code: "invalid", Message: byLoc[l]})
	}
	return out2
}

// Signer signs recipe bytes with Ed25519. The extension holds the public
// key (bundled at build time) and verifies before parsing.
type Signer struct {
	KeyID string
	key   ed25519.PrivateKey
}

// NewSigner builds a signer from a base64 32-byte seed.
func NewSigner(keyID, seedB64 string) (*Signer, error) {
	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("browser: the recipe signing key must be a base64 32-byte Ed25519 seed")
	}
	if keyID == "" {
		return nil, errors.New("browser: the recipe signing key needs a key ID")
	}
	return &Signer{KeyID: keyID, key: ed25519.NewKeyFromSeed(seed)}, nil
}

// Sign returns the base64 signature of b.
func (s *Signer) Sign(b []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(s.key, b))
}

// PublicKey returns the base64 public key (for building the extension).
func (s *Signer) PublicKey() string {
	return base64.StdEncoding.EncodeToString(s.key.Public().(ed25519.PublicKey))
}
