package runner

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/CCH-HQ/cchp-cli/internal/assets"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

var (
	schemaMu    sync.Mutex
	schemaCache = map[string]*jsonschema.Schema{}
)

func bodySchema(opID string) (*jsonschema.Schema, error) {
	schemaMu.Lock()
	defer schemaMu.Unlock()
	if s, ok := schemaCache[opID]; ok {
		return s, nil
	}
	raw := assets.Schemas(opID).Body
	if len(raw) == 0 {
		schemaCache[opID] = nil
		return nil, nil
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	url := "cchp://schemas/" + opID + "/body.json"
	if err := c.AddResource(url, doc); err != nil {
		return nil, err
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compiling the %s body schema: %w", opID, err)
	}
	schemaCache[opID] = s
	return s, nil
}

// schemaErrors validates doc against the operation's request body schema and
// returns one field error per failing leaf, located like body.items[0].name.
func schemaErrors(opID string, doc any) ([]contract.FieldError, error) {
	s, err := bodySchema(opID)
	if err != nil || s == nil {
		return nil, err
	}
	err = s.Validate(doc)
	if err == nil {
		return nil, nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return nil, err
	}
	printer := message.NewPrinter(language.English)
	var out []contract.FieldError
	seen := map[string]bool{}
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			fe := contract.FieldError{Location: fieldPath(e.InstanceLocation), Message: e.ErrorKind.LocalizedString(printer)}
			key := fe.Location + "\x00" + fe.Message
			if !seen[key] {
				seen[key] = true
				out = append(out, fe)
			}
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Location < out[j].Location })
	return out, nil
}

// fieldPath turns instance location tokens such as [items 0 name] into body.items[0].name.
func fieldPath(tokens []string) string {
	var b strings.Builder
	b.WriteString("body")
	for _, tok := range tokens {
		if isIndex(tok) {
			b.WriteString("[" + tok + "]")
			continue
		}
		b.WriteString("." + tok)
	}
	return b.String()
}

func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
