package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/engine"
)

// paged fetches one page by default and up to --page-limit pages on request,
// stopping early at --max-items or at the deadline. The result reports where it
// stopped so the caller can continue explicitly.
func (r *Runner) paged(ctx context.Context, eng *engine.Engine, req engine.Request, o Options, result *contract.Result, params map[string][]string) (*contract.Result, error) {
	op := o.Op
	pg := op.Policy.Pagination
	paramFlag := flagOf(op, pg.Param)
	pageNo := 1
	if pg.Style == "page" {
		if v := first(params[paramFlag]); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "--%s must be a whole number", paramFlag)
			}
			pageNo = n
		}
	}

	limitFlag := ""
	userLimit := 0
	if pg.LimitParam != "" && pg.Style == "cursor" {
		limitFlag = flagOf(op, pg.LimitParam)
		if v := first(params[limitFlag]); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "--%s must be a whole number", limitFlag)
			}
			userLimit = n
		}
	}

	var last map[string]any
	var lastRaw []byte
	var items []any
	pag := &contract.Pagination{}
	for page := 0; page < o.PageLimit; page++ {
		// With --max-items, ask only for what is still needed so the list ends on
		// an item boundary and the next cursor skips nothing.
		if limitFlag != "" && o.MaxItems > 0 {
			remaining := o.MaxItems - len(items)
			switch {
			case userLimit > 0 && remaining < userLimit:
				req.Flags = setFlag(req.Flags, limitFlag, strconv.Itoa(remaining))
			case userLimit == 0 && remaining <= 100:
				req.Flags = setFlag(req.Flags, limitFlag, strconv.Itoa(remaining))
			}
		}
		res, err := eng.Do(ctx, req)
		if err != nil {
			if page > 0 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("stopped after %d pages: %s", page, err.Error()))
				break
			}
			return nil, transportFailure(op, err, "")
		}
		r.trace(op, res)
		if res.Status < 200 || res.Status > 299 {
			return nil, httpFailure(op, res)
		}
		result.Status = res.Status
		result.RequestID = requestID(res)
		doc, err := engine.DecodeJSON(res.Raw)
		if err != nil {
			return nil, fmt.Errorf("the server sent invalid JSON: %w", err)
		}
		body, ok := doc.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s returned a non-object page", op.ID)
		}
		last, lastRaw = body, res.Raw
		pageItems, _ := getPath(body, pg.Items).([]any)
		items = append(items, pageItems...)
		pag.Pages++

		pag.HasMore, pag.NextCursor, pag.NextPage = false, "", 0
		switch pg.Style {
		case "cursor":
			next, _ := getPath(body, pg.Next).(string)
			pag.NextCursor = next
			pag.HasMore = next != ""
			if pg.HasMore != "" {
				more, _ := getPath(body, pg.HasMore).(bool)
				pag.HasMore = more
			}
		case "page":
			total := numberAt(body, pg.Total)
			size := int64(len(pageItems))
			if s := numberAt(body, pg.LimitParam); s != nil {
				size = *s
			}
			if total != nil && size > 0 && int64(pageNo)*size < *total {
				pag.HasMore = true
				pag.NextPage = pageNo + 1
			}
		}
		if t := numberAt(body, pg.Total); t != nil {
			pag.Total = t
		}
		if o.MaxItems > 0 && len(items) >= o.MaxItems {
			if len(items) > o.MaxItems {
				items = items[:o.MaxItems]
				pag.Truncated = true
			}
			break
		}
		if !pag.HasMore {
			break
		}
		if page+1 < o.PageLimit {
			if err := ctx.Err(); err != nil {
				result.Warnings = append(result.Warnings, "stopped at the deadline; continue with the next cursor")
				break
			}
			next := pag.NextCursor
			if pg.Style == "page" {
				pageNo = pag.NextPage
				next = strconv.Itoa(pageNo)
			}
			req.Flags = setFlag(req.Flags, paramFlag, next)
		}
	}
	pag.Items = len(items)
	result.Pagination = pag
	result.OK = true

	if pag.Pages == 1 && !pag.Truncated {
		var buf bytes.Buffer
		if err := json.Compact(&buf, lastRaw); err != nil {
			return nil, err
		}
		result.Data = buf.Bytes()
		return result, nil
	}
	if items == nil {
		items = []any{}
	}
	setPath(last, pg.Items, items)
	data, err := json.Marshal(last)
	if err != nil {
		return nil, err
	}
	result.Data = data
	return result, nil
}

func flagOf(op *catalog.Operation, name string) string {
	for _, p := range op.Params {
		if p.Name == name {
			return p.Flag
		}
	}
	return name
}

func setFlag(flags []engine.Flag, name, value string) []engine.Flag {
	out := flags[:0:0]
	for _, f := range flags {
		if f.Name != name {
			out = append(out, f)
		}
	}
	return append(out, engine.Flag{Name: name, Value: value})
}

func getPath(m map[string]any, path string) any {
	if path == "" {
		return nil
	}
	var cur any = m
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[part]
	}
	return cur
}

func setPath(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	cur := m
	for _, part := range parts[:len(parts)-1] {
		next, ok := cur[part].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = v
}

func numberAt(m map[string]any, path string) *int64 {
	n, ok := getPath(m, path).(json.Number)
	if !ok {
		return nil
	}
	v, err := n.Int64()
	if err != nil {
		return nil
	}
	return &v
}
