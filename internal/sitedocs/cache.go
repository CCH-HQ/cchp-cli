package sitedocs

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/pquerna/cachecontrol/cacheobject"
)

// MaxAge is the longest cchp uses a cached document without asking the site
// again. After that it revalidates the copy with ETag and Last-Modified.
const MaxAge = 5 * time.Minute

// freshness sits between the HTTP cache and the network. It rewrites each
// answer's caching headers into the policy of cchp's private cache: fresh for
// the lifetime the site gives, at most MaxAge, then revalidated; an answer the
// site marks no-cache, or gives no lifetime, is revalidated every time; an
// answer that may not be stored is not stored. Stale copies are never served.
type freshness struct{ next http.RoundTripper }

func (t freshness) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// A 304 revalidates the cached copy, so its headers describe that copy.
	status := resp.StatusCode
	if status == http.StatusNotModified {
		status = http.StatusOK
	}
	reasons, expires, _, obj, err := cacheobject.UsingRequestResponseWithObject(req, status, resp.Header, true)
	if err != nil {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("reading the caching headers of %s: %w", req.URL, err)
	}
	if resp.Header.Get("Date") == "" {
		resp.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}
	resp.Header.Del("Expires")
	lifetime := expires.Sub(obj.NowUTC)
	switch {
	case len(reasons) > 0:
		resp.Header.Set("Cache-Control", "no-store")
	case obj.RespDirectives.NoCachePresent || expires.IsZero() || lifetime < time.Second:
		resp.Header.Set("Cache-Control", "no-cache")
	default:
		resp.Header.Set("Cache-Control", "max-age="+strconv.Itoa(int(min(lifetime, MaxAge)/time.Second)))
	}
	return resp, nil
}
