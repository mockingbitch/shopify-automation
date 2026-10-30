// Package api embeds the OpenAPI document, the source of truth for the HTTP
// contract. Tests validate every response against it.
package api

import _ "embed"

// OpenAPI is the raw OpenAPI 3 document.
//
//go:embed openapi.yaml
var OpenAPI []byte
