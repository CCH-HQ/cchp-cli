// Package assets embeds the files cmd/cchp-gen generates from the pinned spec.
package assets

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
)

//go:embed catalog.json.gz
var catalogGz []byte

//go:embed schemas.json.gz
var schemasGz []byte

// EngineSpecGzip is the gzip-compressed OpenAPI document the Restish engine loads.
//
//go:embed engine-spec.json.gz
var EngineSpecGzip []byte

var (
	catalogOnce = sync.OnceValue(func() *catalog.Catalog {
		c, err := catalog.DecodeGzip(catalogGz)
		if err != nil {
			panic(fmt.Errorf("assets: embedded catalog is unreadable: %w", err))
		}
		return c
	})
	schemasOnce = sync.OnceValue(func() map[string]catalog.OperationSchemas {
		var m map[string]catalog.OperationSchemas
		if err := catalog.DecodeGzipJSON(schemasGz, &m); err != nil {
			panic(fmt.Errorf("assets: embedded schemas are unreadable: %w", err))
		}
		return m
	})
)

// Catalog returns the embedded operation catalog.
func Catalog() *catalog.Catalog { return catalogOnce() }

// Schemas returns the bundled request and response schemas of one operation.
func Schemas(operationID string) catalog.OperationSchemas { return schemasOnce()[operationID] }
