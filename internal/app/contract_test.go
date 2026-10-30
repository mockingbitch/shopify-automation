package app_test

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"shopee-automation/api"
)

var (
	specOnce   sync.Once
	spec       *openapi3.T
	specRouter routers.Router
	specErr    error
)

func loadSpec() (*openapi3.T, routers.Router, error) {
	specOnce.Do(func() {
		loader := openapi3.NewLoader()
		spec, specErr = loader.LoadFromData(api.OpenAPI)
		if specErr != nil {
			return
		}
		if specErr = spec.Validate(context.Background()); specErr != nil {
			return
		}
		// Match paths without the /api/v1 server prefix.
		spec.Servers = nil
		specRouter, specErr = gorillamux.NewRouter(spec)
	})
	return spec, specRouter, specErr
}

// validateResponse checks that the response status and body are documented.
func validateResponse(t *testing.T, req *http.Request, status int, header http.Header, body []byte) {
	t.Helper()
	_, router, err := loadSpec()
	if err != nil {
		t.Fatalf("openapi: %v", err)
	}
	r := req.Clone(context.Background())
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/v1")
	r.URL.Host, r.URL.Scheme, r.Host = "", "", ""
	route, params, err := router.FindRoute(r)
	if err != nil {
		t.Errorf("openapi: %s %s is not documented: %v", req.Method, r.URL.Path, err)
		return
	}
	in := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route},
		Status:                 status,
		Header:                 header,
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	in.SetBodyBytes(body)
	if err := openapi3filter.ValidateResponse(context.Background(), in); err != nil {
		t.Errorf("openapi: %s %s -> %d does not match the spec: %v\nbody: %s", req.Method, r.URL.Path, status, err, body)
	}
}

// TestOpenAPIMatchesRoutes keeps the route table and the document in sync.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	doc, _, err := loadSpec()
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	inCode := map[string]bool{}
	for _, rt := range e.app.Routes() {
		inCode[rt.Method+" "+rt.Path] = true
	}
	inSpec := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			inSpec[method+" "+path] = true
		}
	}
	var missing, extra []string
	for k := range inCode {
		if !inSpec[k] {
			missing = append(missing, k)
		}
	}
	for k := range inSpec {
		if !inCode[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("routes missing from openapi.yaml: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("openapi.yaml documents routes that do not exist: %v", extra)
	}
}
