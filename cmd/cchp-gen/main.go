// Command cchp-gen builds everything cchp derives from the pinned CCHP spec:
//
//   - internal/assets/catalog.json.gz     operation index for discovery, help and policy
//   - internal/assets/schemas.json.gz     bundled request and response JSON Schemas
//   - internal/assets/engine-spec.json.gz the spec the embedded Restish engine loads
//   - docs/coverage.md                    every upstream operation with its outcome
//
// Inputs are spec/upstream/control-plane.yaml (never edited), spec/cli-overlay.yaml
// (an OpenAPI Overlay with CLI names and English text), spec/operation-policy.yaml
// and spec/source.lock.json. With -check it regenerates in memory and fails when a
// committed output differs or an operation has no explained outcome.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/pb33f/libopenapi"
	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/modfile"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/engine"
)

const (
	upstreamPath = "spec/upstream/control-plane.yaml"
	overlayPath  = "spec/cli-overlay.yaml"
	policyPath   = "spec/operation-policy.yaml"
	lockPath     = "spec/source.lock.json"
	catalogOut   = "internal/assets/catalog.json.gz"
	schemasOut   = "internal/assets/schemas.json.gz"
	engineOut    = "internal/assets/engine-spec.json.gz"
	coverageOut  = "docs/coverage.md"
	restishMod   = "github.com/rest-sh/restish/v2"
)

var methods = []string{"get", "put", "post", "patch", "delete"}

// Headers the server or cchp itself manages; they never become CLI flags.
var ignoredHeaders = map[string]bool{
	"cookie": true, "origin": true, "x-csrf-token": true, "sec-fetch-site": true,
	"sec-fetch-mode": true, "sec-fetch-dest": true, "user-agent": true, "content-type": true,
	"x-cch-challenge-proof": true, "x-cch-organization": true, "x-mfa-token": true,
}

// Flags cchp owns on every operation command.
var reservedFlags = map[string]bool{
	"json": true, "dry-run": true, "allow-write": true, "confirm": true, "page-limit": true,
	"max-items": true, "timeout": true, "body-file": true, "form-file": true, "secret-out": true,
	"output": true, "debug": true, "help": true, "max-events": true, "cursor-all": true,
}

func main() {
	check := flag.Bool("check", false, "verify committed outputs are current instead of writing them")
	lock := flag.Bool("lock", false, "rewrite spec/source.lock.json from the current upstream spec")
	tag := flag.String("tag", "", "with -lock: upstream release tag")
	commit := flag.String("commit", "", "with -lock: upstream commit")
	workdir := flag.String("workdir", ".local-e2e/cchp-gen", "scratch directory for the engine compatibility run")
	flag.Parse()

	if *lock {
		if err := writeLock(*tag, *commit); err != nil {
			fail(err)
		}
		return
	}
	outputs, err := generate(*workdir)
	if err != nil {
		fail(err)
	}
	names := make([]string, 0, len(outputs))
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	var stale []string
	for _, name := range names {
		data := outputs[name]
		if *check {
			current, err := os.ReadFile(name)
			if err != nil || !bytes.Equal(current, data) {
				stale = append(stale, name)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(name, data, 0o644); err != nil {
			fail(err)
		}
	}
	if len(stale) > 0 {
		fail(fmt.Errorf("generated files are out of date, run `make generate`: %s", strings.Join(stale, ", ")))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "cchp-gen:", err)
	os.Exit(1)
}

type lockFile struct {
	Upstream struct {
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Commit     string `json:"commit"`
		Exporter   string `json:"exporter"`
	} `json:"upstream"`
	Spec struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"spec"`
	Restish struct {
		Module  string `json:"module"`
		Version string `json:"version"`
	} `json:"restish"`
}

func writeLock(tag, commit string) error {
	if tag == "" || commit == "" {
		return errors.New("-lock needs -tag and -commit")
	}
	var lf lockFile
	lf.Upstream.Repository = "github.com/CCH-HQ/claude-code-hub-plus"
	lf.Upstream.Tag = tag
	lf.Upstream.Commit = commit
	lf.Upstream.Exporter = "go run ./cmd/openapi-export"
	lf.Spec.Path = upstreamPath
	sum, err := fileSHA256(upstreamPath)
	if err != nil {
		return err
	}
	lf.Spec.SHA256 = sum
	lf.Restish.Module = restishMod
	version, err := restishVersion()
	if err != nil {
		return err
	}
	lf.Restish.Version = version
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath, append(data, '\n'), 0o644)
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func restishVersion() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", err
	}
	for _, r := range mf.Require {
		if r.Mod.Path == restishMod {
			return r.Mod.Version, nil
		}
	}
	return "", fmt.Errorf("go.mod does not require %s", restishMod)
}

func readLock() (lockFile, error) {
	var lf lockFile
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return lf, err
	}
	if err := json.Unmarshal(data, &lf); err != nil {
		return lf, err
	}
	sum, err := fileSHA256(lf.Spec.Path)
	if err != nil {
		return lf, err
	}
	if sum != lf.Spec.SHA256 {
		return lf, fmt.Errorf("%s has sha256 %s but %s records %s; the upstream spec must stay exactly as exported (use make sync-spec)", lf.Spec.Path, sum, lockPath, lf.Spec.SHA256)
	}
	version, err := restishVersion()
	if err != nil {
		return lf, err
	}
	if version != lf.Restish.Version {
		return lf, fmt.Errorf("go.mod pins restish %s but %s records %s", version, lockPath, lf.Restish.Version)
	}
	return lf, nil
}

// overlayExtensions are the cchp-specific top-level keys of spec/cli-overlay.yaml.
type overlayExtensions struct {
	Groups []catalog.Group `yaml:"x-cchp-groups"`
}

type policyFile struct {
	Version    int                    `yaml:"version"`
	Operations map[string]policyEntry `yaml:"operations"`
	Defaults   struct{ Risk string }  `yaml:"defaults"`
}

type policyEntry struct {
	Risk         string              `yaml:"risk"`
	Retry        bool                `yaml:"retry"`
	Permission   string              `yaml:"permission"`
	SecretFields []string            `yaml:"secret_fields"`
	Pagination   *catalog.Pagination `yaml:"pagination"`
	Note         string              `yaml:"note"`
}

func generate(workdir string) (map[string][]byte, error) {
	lf, err := readLock()
	if err != nil {
		return nil, err
	}
	upstream, err := os.ReadFile(upstreamPath)
	if err != nil {
		return nil, err
	}
	overlayBytes, err := os.ReadFile(overlayPath)
	if err != nil {
		return nil, err
	}
	merged, err := libopenapi.ApplyOverlayFromBytesToSpecBytes(upstream, overlayBytes)
	if err != nil {
		return nil, fmt.Errorf("apply %s: %w", overlayPath, err)
	}
	for _, w := range merged.Warnings {
		return nil, fmt.Errorf("%s: action %q: %s", overlayPath, w.Target, w.Message)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(merged.Bytes, &doc); err != nil {
		return nil, err
	}
	var ext overlayExtensions
	if err := yaml.Unmarshal(overlayBytes, &ext); err != nil {
		return nil, err
	}
	var pol policyFile
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(policyBytes))
	dec.KnownFields(true)
	if err := dec.Decode(&pol); err != nil {
		return nil, fmt.Errorf("%s: %w", policyPath, err)
	}
	if pol.Defaults.Risk != catalog.RiskWrite {
		return nil, fmt.Errorf("%s: defaults.risk must be %q so unclassified operations are treated as writes", policyPath, catalog.RiskWrite)
	}

	g := &generator{doc: doc, policy: pol, groups: map[string]catalog.Group{}}
	for _, grp := range ext.Groups {
		g.groups[grp.Name] = grp
	}
	if err := g.build(); err != nil {
		return nil, err
	}
	if err := g.engineCompatibility(workdir); err != nil {
		return nil, err
	}

	cat := catalog.Catalog{
		Source: catalog.Source{
			Repository: lf.Upstream.Repository, Tag: lf.Upstream.Tag, Commit: lf.Upstream.Commit,
			SpecPath: lf.Spec.Path, SpecSHA256: lf.Spec.SHA256, Exporter: lf.Upstream.Exporter,
			RestishModule: lf.Restish.Module, RestishVersion: lf.Restish.Version,
		},
		Groups:     ext.Groups,
		Operations: g.ops,
	}
	catJSON, err := json.Marshal(cat)
	if err != nil {
		return nil, err
	}
	schemasJSON, err := json.Marshal(g.schemas)
	if err != nil {
		return nil, err
	}
	engineJSON, err := json.Marshal(g.engineSpec(g.available()))
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	if out[catalogOut], err = gz(catJSON); err != nil {
		return nil, err
	}
	if out[schemasOut], err = gz(schemasJSON); err != nil {
		return nil, err
	}
	if out[engineOut], err = gz(engineJSON); err != nil {
		return nil, err
	}
	out[coverageOut] = []byte(coverage(&cat))
	return out, nil
}

func gz(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type rawOp struct {
	method string
	path   string
	node   map[string]any
	params []map[string]any
}

type generator struct {
	doc     map[string]any
	policy  policyFile
	groups  map[string]catalog.Group
	ops     []*catalog.Operation
	raw     map[string]rawOp
	schemas map[string]catalog.OperationSchemas
}

func (g *generator) build() error {
	paths, _ := g.doc["paths"].(map[string]any)
	pathKeys := sortedKeys(paths)
	g.raw = map[string]rawOp{}
	g.schemas = map[string]catalog.OperationSchemas{}
	var problems []string
	seenCommand := map[string]string{}
	for _, p := range pathKeys {
		item, _ := paths[p].(map[string]any)
		pathParams := listOfMaps(item["parameters"])
		for _, m := range methods {
			node, ok := item[m].(map[string]any)
			if !ok {
				continue
			}
			op, raw, errs := g.operation(m, p, node, pathParams)
			for _, e := range errs {
				problems = append(problems, fmt.Sprintf("%s (%s %s): %s", op.ID, strings.ToUpper(m), p, e))
			}
			if prev, dup := seenCommand[op.Command()]; dup && op.Status == catalog.StatusAvailable {
				problems = append(problems, fmt.Sprintf("%s: command %q is already used by %s; set x-cli-name in %s", op.ID, op.Command(), prev, overlayPath))
			}
			if op.Status == catalog.StatusAvailable {
				seenCommand[op.Command()] = op.ID
			}
			g.ops = append(g.ops, op)
			g.raw[op.ID] = raw
		}
	}
	for id := range g.policy.Operations {
		if _, ok := g.raw[id]; !ok {
			problems = append(problems, fmt.Sprintf("%s lists %s, which is not in the upstream spec", policyPath, id))
		}
	}
	sort.SliceStable(g.ops, func(i, j int) bool {
		if g.ops[i].Group != g.ops[j].Group {
			return g.ops[i].Group < g.ops[j].Group
		}
		return g.ops[i].Name < g.ops[j].Name
	})
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New("unexplained or invalid operations:\n  " + strings.Join(problems, "\n  "))
	}
	return nil
}

var hanRe = regexp.MustCompile(`\p{Han}`)

func hasHan(s string) bool { return hanRe.MatchString(s) }

func (g *generator) operation(method, path string, node map[string]any, pathParams []map[string]any) (*catalog.Operation, rawOp, []string) {
	var errs []string
	id := str(node["operationId"])
	op := &catalog.Operation{ID: id, Method: strings.ToUpper(method), Path: path}
	op.Group = str(node["x-cchp-group"])
	op.Name = str(node["x-cli-name"])
	if op.Name == "" {
		op.Name = kebab(id)
	}
	op.Aliases = strs(node["x-cli-aliases"])
	op.Summary = str(node["x-cli-description"])
	if op.Group == "" {
		errs = append(errs, "x-cchp-group is missing in "+overlayPath)
	} else if _, ok := g.groups[op.Group]; !ok {
		errs = append(errs, fmt.Sprintf("group %q is not declared in x-cchp-groups", op.Group))
	}
	if op.Summary == "" || hasHan(op.Summary) {
		errs = append(errs, "x-cli-description must hold an English summary")
	}

	params := mergeParams(pathParams, listOfMaps(node["parameters"]))
	security, hasSecurity := node["security"]
	alternatives := listOfMaps(security)
	management := false
	public := hasSecurity && len(alternatives) == 0
	var schemes []string
	for _, alt := range alternatives {
		keys := sortedKeys(alt)
		schemes = append(schemes, strings.Join(keys, "+"))
		if len(keys) == 1 && keys[0] == "managementBearer" {
			management = true
		}
	}
	if !hasSecurity && strings.HasPrefix(path, "/api/") {
		// Declared without a security key: the server's auth chain covers every /api
		// path, so a management token authenticates and its permissions decide.
		management = true
	}
	op.Public = public

	ignore := truthy(node["x-cli-ignore"])
	excludeReason := str(node["x-cchp-exclude-reason"])
	unsupportedReason := str(node["x-cchp-unsupported-reason"])
	switch {
	case unsupportedReason != "":
		op.Status, op.Reason = catalog.StatusUnsupported, unsupportedReason
	case ignore:
		op.Status, op.Reason = catalog.StatusExcluded, excludeReason
		if excludeReason == "" {
			errs = append(errs, "x-cli-ignore needs x-cchp-exclude-reason")
		}
	case !management && !public:
		op.Status = catalog.StatusExcluded
		op.Reason = securityReason(schemes)
	default:
		op.Status = catalog.StatusAvailable
	}
	if (op.Status == catalog.StatusExcluded || op.Status == catalog.StatusUnsupported) && hasHan(op.Reason) {
		errs = append(errs, "the exclusion reason must be English")
	}

	raw := rawOp{method: method, path: path, node: node}
	for _, p := range params {
		name, in := str(p["name"]), str(p["in"])
		if in == "header" && strings.EqualFold(name, "X-CCH-Organization") {
			op.OrgHeader = true
			continue
		}
		if in == "header" && strings.EqualFold(name, "Idempotency-Key") {
			op.Idempotency = true
		}
		if in == "header" && ignoredHeaders[strings.ToLower(name)] {
			continue
		}
		if in == "cookie" || truthy(p["x-cli-ignore"]) {
			continue
		}
		schema, _ := p["schema"].(map[string]any)
		cp := catalog.Param{Name: name, In: in, Required: truthy(p["required"]) || in == "path"}
		cp.Type, cp.ItemType = schemaType(schema)
		cp.Enum = enumStrings(schema)
		if d, ok := schema["default"]; ok {
			cp.Default = d
		}
		cp.Flag = str(p["x-cli-name"])
		if cp.Flag == "" {
			cp.Flag = flagName(name, in)
		}
		cp.Description = str(p["x-cli-description"])
		if cp.Description == "" && !hasHan(str(p["description"])) {
			cp.Description = str(p["description"])
		}
		for _, prev := range op.Params {
			if prev.Flag == cp.Flag {
				errs = append(errs, fmt.Sprintf("parameters %q and %q both map to --%s; set x-cli-name or x-cli-ignore on one", prev.Name, name, cp.Flag))
			}
		}
		if op.Status == catalog.StatusAvailable {
			if cp.Description == "" || hasHan(cp.Description) {
				errs = append(errs, fmt.Sprintf("parameter %q needs an English x-cli-description", name))
			}
			if reservedFlags[cp.Flag] && cp.Flag != "idempotency-key" {
				errs = append(errs, fmt.Sprintf("parameter %q maps to reserved flag --%s; set x-cli-name on it", name, cp.Flag))
			}
		}
		op.Params = append(op.Params, cp)
		raw.params = append(raw.params, p)
	}

	op.Body = requestBody(node)
	op.Response = responseInfo(node)
	if op.Body != nil && op.Body.Kind == "form" && op.Status == catalog.StatusAvailable {
		op.Status, op.Reason = catalog.StatusUnsupported, "Form-encoded request bodies belong to browser sign-in flows; cchp sends JSON, multipart and binary bodies only."
	}

	pe, classified := g.policy.Operations[id]
	op.Policy = catalog.Policy{Risk: catalog.RiskWrite}
	if classified {
		op.Policy = catalog.Policy{
			Risk: pe.Risk, Classified: true, Retry: pe.Retry, Permission: pe.Permission,
			SecretFields: pe.SecretFields, Pagination: pe.Pagination, Note: pe.Note,
		}
		switch pe.Risk {
		case catalog.RiskRead, catalog.RiskWrite, catalog.RiskDestructive:
		default:
			errs = append(errs, fmt.Sprintf("policy risk %q is not read, write or destructive", pe.Risk))
		}
		if pe.Retry && op.Method != "GET" && pe.Risk != catalog.RiskRead {
			errs = append(errs, "only reads may be retried automatically")
		}
		if hasHan(pe.Note) {
			errs = append(errs, "policy note must be English")
		}
	}

	if op.Status == catalog.StatusAvailable {
		body, resp, schemaErrs := g.operationSchemas(node)
		errs = append(errs, schemaErrs...)
		g.schemas[id] = catalog.OperationSchemas{Body: body, Response: resp}
		errs = append(errs, validatePolicy(op, resp)...)
	}
	return op, raw, errs
}

func securityReason(schemes []string) string {
	all := strings.Join(schemes, " ")
	switch {
	case strings.Contains(all, "setupInstallerCookie"):
		return "First-time installation step that runs in the browser with the installer session."
	case strings.Contains(all, "llmKeySession"):
		return "Works only inside the read-only browser session opened with an LLM token."
	case strings.Contains(all, "anonymousCSRFCookie"):
		return "Part of the browser sign-in flow; it needs the browser's pre-sign-in cookie."
	default:
		return "The server accepts only a signed-in browser session for this operation, not a management token."
	}
}

func validatePolicy(op *catalog.Operation, resp json.RawMessage) []string {
	var errs []string
	var root map[string]any
	if len(resp) > 0 {
		if err := json.Unmarshal(resp, &root); err != nil {
			return []string{err.Error()}
		}
	}
	has := func(path string) bool { return root != nil && schemaHasPath(root, root, strings.Split(path, ".")) }
	for _, f := range op.Policy.SecretFields {
		if !has(f) {
			errs = append(errs, fmt.Sprintf("policy secret field %q is not in the response schema", f))
		}
	}
	if pg := op.Policy.Pagination; pg != nil {
		if !hasParam(op, pg.Param) {
			errs = append(errs, fmt.Sprintf("pagination parameter %q is not a parameter", pg.Param))
		}
		if pg.LimitParam != "" && !hasParam(op, pg.LimitParam) {
			errs = append(errs, fmt.Sprintf("pagination limit parameter %q is not a parameter", pg.LimitParam))
		}
		for _, f := range []string{pg.Next, pg.Items, pg.Total, pg.HasMore} {
			if f != "" && !has(f) {
				errs = append(errs, fmt.Sprintf("pagination field %q is not in the response schema", f))
			}
		}
		if pg.Style != "cursor" && pg.Style != "page" {
			errs = append(errs, "pagination style must be cursor or page")
		}
	}
	return errs
}

func hasParam(op *catalog.Operation, name string) bool {
	for _, p := range op.Params {
		if p.Name == name {
			return true
		}
	}
	return false
}

// schemaHasPath reports whether a dotted property path exists in a bundled
// schema, following $defs references and oneOf alternatives.
func schemaHasPath(root, node map[string]any, path []string) bool {
	if len(path) == 0 {
		return true
	}
	if ref := str(node["$ref"]); strings.HasPrefix(ref, "#/$defs/") {
		defs, _ := root["$defs"].(map[string]any)
		def, _ := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		return def != nil && schemaHasPath(root, def, path)
	}
	for _, alt := range listOfMaps(node["oneOf"]) {
		if schemaHasPath(root, alt, path) {
			return true
		}
	}
	props, _ := node["properties"].(map[string]any)
	child, ok := props[path[0]].(map[string]any)
	return ok && schemaHasPath(root, child, path[1:])
}

func requestBody(node map[string]any) *catalog.Body {
	rb, ok := node["requestBody"].(map[string]any)
	if !ok {
		return nil
	}
	content, _ := rb["content"].(map[string]any)
	cts := sortedKeys(content)
	if len(cts) == 0 {
		return nil
	}
	ct := cts[0]
	for _, c := range cts {
		if c == "application/json" {
			ct = c
		}
	}
	b := &catalog.Body{ContentType: ct, Required: truthy(rb["required"])}
	switch {
	case ct == "application/json" || strings.HasSuffix(ct, "+json"):
		b.Kind = "json"
	case ct == "multipart/form-data":
		b.Kind = "multipart"
		media, _ := content[ct].(map[string]any)
		schema, _ := media["schema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		for _, name := range sortedKeys(props) {
			prop, _ := props[name].(map[string]any)
			if str(prop["format"]) == "binary" {
				b.FileFields = append(b.FileFields, name)
			}
		}
	case ct == "application/x-www-form-urlencoded":
		b.Kind = "form"
	default:
		b.Kind = "binary"
	}
	return b
}

func responseInfo(node map[string]any) catalog.Response {
	responses, _ := node["responses"].(map[string]any)
	seen := map[string]bool{}
	var cts []string
	for _, code := range sortedKeys(responses) {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		resp, _ := responses[code].(map[string]any)
		content, _ := resp["content"].(map[string]any)
		for _, ct := range sortedKeys(content) {
			if !seen[ct] {
				seen[ct] = true
				cts = append(cts, ct)
			}
		}
	}
	r := catalog.Response{Kind: catalog.ResponseEmpty, ContentTypes: cts}
	for _, ct := range cts {
		switch {
		case ct == "text/event-stream":
			r.Kind = catalog.ResponseStream
		case ct == "application/json" || strings.HasSuffix(ct, "+json"):
			if r.Kind == catalog.ResponseEmpty {
				r.Kind = catalog.ResponseJSON
			}
		default:
			if r.Kind != catalog.ResponseStream {
				r.Kind = catalog.ResponseBinary
			}
		}
	}
	return r
}

// operationSchemas bundles the JSON request body and JSON success response
// schemas with their referenced components under $defs.
func (g *generator) operationSchemas(node map[string]any) (json.RawMessage, json.RawMessage, []string) {
	var errs []string
	var body, resp json.RawMessage
	if rb, ok := node["requestBody"].(map[string]any); ok {
		content, _ := rb["content"].(map[string]any)
		for _, ct := range sortedKeys(content) {
			if ct != "application/json" && ct != "multipart/form-data" {
				continue
			}
			media, _ := content[ct].(map[string]any)
			if schema, ok := media["schema"].(map[string]any); ok {
				data, err := g.bundle(schema)
				if err != nil {
					errs = append(errs, err.Error())
				}
				body = data
			}
		}
	}
	// Every JSON success response; several of them become one oneOf schema.
	responses, _ := node["responses"].(map[string]any)
	var alternatives []any
	for _, code := range sortedKeys(responses) {
		if !strings.HasPrefix(code, "2") {
			continue
		}
		r, _ := responses[code].(map[string]any)
		content, _ := r["content"].(map[string]any)
		media, ok := content["application/json"].(map[string]any)
		if !ok {
			continue
		}
		if schema, ok := media["schema"].(map[string]any); ok {
			alternatives = append(alternatives, schema)
		}
	}
	switch len(alternatives) {
	case 0:
	case 1:
		data, err := g.bundle(alternatives[0].(map[string]any))
		if err != nil {
			errs = append(errs, err.Error())
		}
		resp = data
	default:
		data, err := g.bundle(map[string]any{"oneOf": alternatives})
		if err != nil {
			errs = append(errs, err.Error())
		}
		resp = data
	}
	return body, resp, errs
}

func (g *generator) bundle(root map[string]any) (json.RawMessage, error) {
	components, _ := g.doc["components"].(map[string]any)
	all, _ := components["schemas"].(map[string]any)
	defs := map[string]any{}
	var visit func(v any) (any, error)
	visit = func(v any) (any, error) {
		switch t := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, val := range t {
				if k == "$ref" {
					ref := str(val)
					name := strings.TrimPrefix(ref, "#/components/schemas/")
					if name == ref {
						return nil, fmt.Errorf("unsupported $ref %q", ref)
					}
					if _, done := defs[name]; !done {
						target, ok := all[name]
						if !ok {
							return nil, fmt.Errorf("missing schema %q", name)
						}
						defs[name] = nil
						resolved, err := visit(target)
						if err != nil {
							return nil, err
						}
						defs[name] = resolved
					}
					out["$ref"] = "#/$defs/" + name
					continue
				}
				if (k == "description" || k == "title") && hasHan(str(val)) {
					continue
				}
				if k == "examples" {
					if data, err := json.Marshal(val); err == nil && hasHan(string(data)) {
						continue
					}
				}
				nv, err := visit(val)
				if err != nil {
					return nil, err
				}
				out[k] = nv
			}
			return out, nil
		case []any:
			out := make([]any, len(t))
			for i, val := range t {
				nv, err := visit(val)
				if err != nil {
					return nil, err
				}
				out[i] = nv
			}
			return out, nil
		default:
			return v, nil
		}
	}
	resolved, err := visit(root)
	if err != nil {
		return nil, err
	}
	m, _ := resolved.(map[string]any)
	m["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	if len(defs) > 0 {
		m["$defs"] = defs
	}
	return json.Marshal(m)
}

// engineCompatibility runs every available operation through the embedded
// Restish engine and marks the ones it cannot build as unsupported.
func (g *generator) engineCompatibility(workdir string) error {
	specJSON, err := json.Marshal(g.engineSpec(g.available()))
	if err != nil {
		return err
	}
	specGz, err := gz(specJSON)
	if err != nil {
		return err
	}
	dir, err := filepath.Abs(workdir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	names, warnings, err := engine.Commands(dir, specGz)
	if err != nil {
		return fmt.Errorf("engine compatibility run: %w: %s", err, warnings)
	}
	built := map[string]bool{}
	for _, n := range names {
		built[n] = true
	}
	for _, op := range g.ops {
		if op.Status != catalog.StatusAvailable || built[op.ID] {
			continue
		}
		reason := "The embedded Restish engine could not build a command for this operation."
		for _, line := range strings.Split(warnings, "\n") {
			if strings.Contains(line, op.Path) || strings.Contains(line, op.ID) {
				reason += " Engine message: " + strings.TrimSpace(line)
				break
			}
		}
		op.Status, op.Reason = catalog.StatusUnsupported, reason
		delete(g.schemas, op.ID)
	}
	return nil
}

func coverage(c *catalog.Catalog) string {
	var available, excluded, unsupported []*catalog.Operation
	for _, op := range c.Operations {
		switch op.Status {
		case catalog.StatusAvailable:
			available = append(available, op)
		case catalog.StatusExcluded:
			excluded = append(excluded, op)
		default:
			unsupported = append(unsupported, op)
		}
	}
	var b strings.Builder
	b.WriteString("# Coverage\n\n")
	b.WriteString("<!-- Generated by `make generate` (cmd/cchp-gen). Do not edit by hand. -->\n\n")
	fmt.Fprintf(&b, "Every operation in the pinned CCHP control-plane spec (%s, `%s`) has one outcome.\n\n", c.Source.Tag, c.Source.SpecSHA256[:12])
	fmt.Fprintf(&b, "- **Available:** %d. cchp can call them with a management token, or without one when the server marks them public.\n", len(available))
	fmt.Fprintf(&b, "- **Excluded:** %d. The server does not accept a management token for them, or they only make sense in a browser.\n", len(excluded))
	fmt.Fprintf(&b, "- **Unsupported:** %d. The server accepts a management token for them; cchp cannot send their requests yet.\n", len(unsupported))
	fmt.Fprintf(&b, "- **Total:** %d.\n\n", len(c.Operations))
	b.WriteString("Risk shows how agent mode treats a call: `read` runs by default, `write` needs `--allow-write`, `destructive` also needs `--confirm <operationId>`. A `secret` mark means the response holds a one-time secret that needs `--secret-out`.\n\n")

	b.WriteString("## Available\n")
	group := ""
	for _, op := range available {
		if op.Group != group {
			group = op.Group
			g, _ := c.Group(group)
			fmt.Fprintf(&b, "\n### %s\n\n", g.Title)
		}
		marks := op.Policy.Risk
		if !op.Policy.Classified {
			marks += ", unclassified"
		}
		if op.Policy.SecretOutput() {
			marks += ", secret"
		}
		if op.Public {
			marks += ", public"
		}
		fmt.Fprintf(&b, "- `cchp %s` (`%s`, %s): %s\n", op.Command(), op.ID, marks, op.Summary)
	}
	b.WriteString("\n## Excluded\n\n")
	for _, op := range excluded {
		fmt.Fprintf(&b, "- `%s` (%s `%s`): %s Reason: %s\n", op.ID, op.Method, op.Path, op.Summary, op.Reason)
	}
	b.WriteString("\n## Unsupported\n\n")
	if len(unsupported) == 0 {
		b.WriteString("None.\n")
	}
	for _, op := range unsupported {
		fmt.Fprintf(&b, "- `%s` (%s `%s`): %s Reason: %s\n", op.ID, op.Method, op.Path, op.Summary, op.Reason)
	}
	return b.String()
}

func (g *generator) available() []*catalog.Operation {
	var out []*catalog.Operation
	for _, op := range g.ops {
		if op.Status == catalog.StatusAvailable {
			out = append(out, op)
		}
	}
	return out
}

// engineSpec is the slim OpenAPI document the Restish engine loads: available
// operations only, named by operationId, every non-path parameter as a flag,
// and schemas reduced to what request building needs.
func (g *generator) engineSpec(ops []*catalog.Operation) map[string]any {
	paths := map[string]any{}
	for _, op := range ops {
		raw := g.raw[op.ID]
		item, _ := paths[raw.path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[raw.path] = item
		}
		var params []any
		for i, p := range op.Params {
			rp := raw.params[i]
			ep := map[string]any{
				"name":       p.Name,
				"in":         p.In,
				"required":   p.In == "path",
				"x-cli-name": p.Flag,
				"schema":     slimSchema(rp["schema"]),
			}
			for _, k := range []string{"style", "explode", "allowReserved"} {
				if v, ok := rp[k]; ok {
					ep[k] = v
				}
			}
			params = append(params, ep)
		}
		eo := map[string]any{
			"operationId": op.ID,
			"x-cli-name":  op.ID,
			"summary":     op.Summary,
			"responses":   slimResponses(raw.node["responses"]),
		}
		if len(params) > 0 {
			eo["parameters"] = params
		}
		if sec, ok := raw.node["security"]; ok {
			eo["security"] = sec
		}
		if rb, ok := raw.node["requestBody"].(map[string]any); ok {
			eo["requestBody"] = slimRequestBody(rb)
		}
		item[raw.method] = eo
	}
	components, _ := g.doc["components"].(map[string]any)
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "CCHP control plane", "version": "engine"},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": components["securitySchemes"],
		},
	}
}

func slimSchema(v any) map[string]any {
	s, _ := v.(map[string]any)
	out := map[string]any{}
	t, item := schemaType(s)
	out["type"] = t
	if t == "array" {
		out["items"] = map[string]any{"type": item}
	}
	if e, ok := s["enum"]; ok {
		out["enum"] = e
	}
	return out
}

func slimRequestBody(rb map[string]any) map[string]any {
	content, _ := rb["content"].(map[string]any)
	out := map[string]any{}
	for _, ct := range sortedKeys(content) {
		media, _ := content[ct].(map[string]any)
		switch ct {
		case "multipart/form-data":
			schema, _ := media["schema"].(map[string]any)
			props, _ := schema["properties"].(map[string]any)
			slimProps := map[string]any{}
			for name, p := range props {
				pm, _ := p.(map[string]any)
				slimProps[name] = map[string]any{"type": "string", "format": str(pm["format"])}
			}
			m := map[string]any{"schema": map[string]any{"type": "object", "properties": slimProps}}
			if enc, ok := media["encoding"]; ok {
				m["encoding"] = enc
			}
			out[ct] = m
		case "application/json":
			out[ct] = map[string]any{"schema": map[string]any{}}
		default:
			out[ct] = map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}
		}
	}
	return map[string]any{"required": truthy(rb["required"]), "content": out}
}

func slimResponses(v any) map[string]any {
	responses, _ := v.(map[string]any)
	out := map[string]any{}
	for code, r := range responses {
		rm, _ := r.(map[string]any)
		entry := map[string]any{"description": code}
		if content, ok := rm["content"].(map[string]any); ok {
			slim := map[string]any{}
			for ct := range content {
				slim[ct] = map[string]any{"schema": map[string]any{}}
			}
			entry["content"] = slim
		}
		out[code] = entry
	}
	return out
}

func schemaType(s map[string]any) (string, string) {
	t := typeName(s["type"])
	if t == "" {
		t = "string"
	}
	item := ""
	if t == "array" {
		items, _ := s["items"].(map[string]any)
		item = typeName(items["type"])
		if item == "" {
			item = "string"
		}
	}
	return t, item
}

func typeName(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		for _, x := range t {
			if s := str(x); s != "null" {
				return s
			}
		}
	}
	return ""
}

func enumStrings(s map[string]any) []string {
	var out []string
	for _, e := range listOf(s["enum"]) {
		out = append(out, fmt.Sprint(e))
	}
	return out
}

func mergeParams(pathParams, opParams []map[string]any) []map[string]any {
	var out []map[string]any
	key := func(p map[string]any) string { return str(p["in"]) + ":" + str(p["name"]) }
	overridden := map[string]bool{}
	for _, p := range opParams {
		overridden[key(p)] = true
	}
	for _, p := range pathParams {
		if !overridden[key(p)] {
			out = append(out, p)
		}
	}
	out = append(out, opParams...)
	// Path parameters first, in template order; the rest keep spec order.
	sort.SliceStable(out, func(i, j int) bool {
		return str(out[i]["in"]) == "path" && str(out[j]["in"]) != "path"
	})
	return out
}

// kebab turns an operationId such as listOrgProviders into list-org-providers.
func kebab(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			prevUpper := i > 0 && unicode.IsUpper(runes[i-1])
			if i > 0 && (prevLower || (prevUpper && nextLower)) {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		if r == '_' || r == ' ' || r == '.' {
			b.WriteByte('-')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func flagName(name, in string) string {
	if in == "header" {
		n := strings.ToLower(name)
		return strings.TrimPrefix(n, "x-")
	}
	return kebab(name)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func strs(v any) []string {
	var out []string
	for _, x := range listOf(v) {
		out = append(out, str(x))
	}
	return out
}

func truthy(v any) bool {
	b, _ := v.(bool)
	return b
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

func listOfMaps(v any) []map[string]any {
	var out []map[string]any
	for _, x := range listOf(v) {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
