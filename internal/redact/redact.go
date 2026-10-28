// Package redact is the one redaction policy for everything cchp prints or
// saves outside an explicit secret destination: debug traces, dry runs, error
// messages, diagnostics and saved output.
package redact

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Mask replaces a secret value.
const Mask = "[redacted]"

var tokenRe = regexp.MustCompile(`\bcch_(auth|llm)_live_([A-Za-z0-9]+)_[A-Za-z0-9_\-]+`)

// Matches LLM-provider style keys and bearer values that may appear in errors.
var bearerRe = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9._~+/\-]{8,}=*`)

var secretHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "cookie": true, "set-cookie": true,
	"x-csrf-token": true, "x-cch-challenge-proof": true, "x-mfa-token": true, "x-api-key": true,
}

// secretFieldRe matches JSON field names whose string values are credentials.
var secretFieldRe = regexp.MustCompile(`(?i)^(plaintext|lmtPlaintext|secrets|.*secret|.*secret_?(access_?)?key|.*api_?key|.*private_?key|.*password|.*token|recovery_codes|codes|proof)$`)

// String removes CCHP tokens and bearer credentials from free text.
func String(s string) string {
	s = tokenRe.ReplaceAllString(s, "cch_${1}_live_${2}_"+Mask)
	return bearerRe.ReplaceAllString(s, "${1} "+Mask)
}

// Header returns a copy of h with credential headers masked.
func Header(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vals := range h {
		cp := make([]string, len(vals))
		for i, v := range vals {
			if secretHeaders[strings.ToLower(k)] {
				cp[i] = Mask
				if strings.EqualFold(k, "Authorization") && strings.HasPrefix(v, "Bearer ") {
					cp[i] = "Bearer " + Mask
				}
			} else {
				cp[i] = String(v)
			}
		}
		out[k] = cp
	}
	return out
}

// HeaderMap flattens a header for JSON output with masked credentials.
func HeaderMap(h http.Header) map[string]string {
	red := Header(h)
	keys := make([]string, 0, len(red))
	for k := range red {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = strings.Join(red[k], ", ")
	}
	return out
}

// IsSecretField reports whether a JSON field name holds a credential.
func IsSecretField(name string) bool { return secretFieldRe.MatchString(name) }

// Value masks credential fields anywhere inside a decoded JSON value. extra
// names more fields to mask. The input is not modified.
func Value(v any, extra ...string) any {
	extraSet := map[string]bool{}
	for _, e := range extra {
		extraSet[e] = true
	}
	return value(v, extraSet)
}

func value(v any, extra map[string]bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if (IsSecretField(k) || extra[k]) && isScalarSecret(val) {
				out[k] = Mask
				continue
			}
			out[k] = value(val, extra)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = value(val, extra)
		}
		return out
	case string:
		return String(t)
	default:
		return v
	}
}

func isScalarSecret(v any) bool {
	switch t := v.(type) {
	case string:
		return t != ""
	case []any:
		for _, x := range t {
			if _, ok := x.(string); !ok {
				return false
			}
		}
		return len(t) > 0
	case json.Number, float64, bool, nil:
		return false
	}
	return false
}

// JSON masks credential fields in a JSON document.
func JSON(data []byte, extra ...string) ([]byte, error) {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(Value(v, extra...))
}
