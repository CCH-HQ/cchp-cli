package main

import (
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
)

func content(types map[string]map[string]any) map[string]any {
	c := map[string]any{}
	for ct, schema := range types {
		c[ct] = map[string]any{"schema": schema}
	}
	return map[string]any{"responses": map[string]any{"200": map[string]any{"content": c}}}
}

var binarySchema = map[string]any{"type": "string", "format": "binary"}

func TestResponseInfoClassifiesBinarySchemasAsDownloads(t *testing.T) {
	cases := []struct {
		name string
		node map[string]any
		want string
	}{
		{"octet stream", content(map[string]map[string]any{"application/octet-stream": binarySchema}), catalog.ResponseBinary},
		// An archived part is served with its own type; its bytes are still a file.
		{"file served as SSE, NDJSON or JSON", content(map[string]map[string]any{
			"application/json": binarySchema, "text/event-stream": binarySchema,
			"application/x-ndjson": binarySchema, "application/octet-stream": binarySchema, "*/*": binarySchema,
		}), catalog.ResponseBinary},
		{"file declared as JSON", content(map[string]map[string]any{"application/json": binarySchema}), catalog.ResponseBinary},
		{"event stream", content(map[string]map[string]any{"text/event-stream": {"type": "string"}}), catalog.ResponseStream},
		{"json", content(map[string]map[string]any{"application/json": {"type": "object"}}), catalog.ResponseJSON},
		{"no content", map[string]any{"responses": map[string]any{"204": map[string]any{}}}, catalog.ResponseEmpty},
	}
	for _, c := range cases {
		if got := responseInfo(c.node).Kind; got != c.want {
			t.Errorf("%s: kind %s, want %s", c.name, got, c.want)
		}
	}
}
