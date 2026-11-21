package selfupdate

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/redact"
)

const (
	DefaultAPIBaseURL = "https://api.github.com"

	acceptGitHubJSON = "application/vnd.github+json"
	maxMetadataBytes = 2 << 20   // 2 MiB
	maxChecksumBytes = 1 << 20   // 1 MiB
	maxArchiveBytes  = 128 << 20 // 128 MiB
)

// Release is the subset of a GitHub Release response used by cchp update.
type Release struct {
	TagName    string  `json:"tag_name"`
	HTMLURL    string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset is one downloadable release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// FindAsset returns the asset matching name exactly.
func (r *Release) FindAsset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// ArchiveName returns the GoReleaser archive filename for a release tag and platform.
// Only the six platforms built by .goreleaser.yaml are supported.
func ArchiveName(tag, goos, goarch string) (string, bool) {
	norm, ok := NormalizeVersion(tag)
	if !ok {
		return "", false
	}
	if !isSupportedPlatform(goos, goarch) {
		return "", false
	}
	ver := strings.TrimPrefix(norm, "v")
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("cchp_%s_%s_%s%s", ver, goos, goarch, ext), true
}

func isSupportedPlatform(goos, goarch string) bool {
	switch goos {
	case "linux", "darwin", "windows":
	default:
		return false
	}
	switch goarch {
	case "amd64", "arm64":
		return true
	default:
		return false
	}
}

// BinaryName returns the expected executable filename inside the release archive.
func BinaryName(goos string) string {
	if goos == "windows" {
		return "cchp.exe"
	}
	return "cchp"
}

// releaseClient fetches public GitHub Releases metadata and assets without
// ever sending credentials.
type releaseClient struct {
	apiBaseURL    string
	repo          string
	userAgent     string
	httpClient    *http.Client
	allowLoopback bool
	debug         io.Writer
}

func newReleaseClient(apiBaseURL, repo, userAgent string, baseClient *http.Client, retryMax int, debug io.Writer) *releaseClient {
	if apiBaseURL == "" {
		apiBaseURL = DefaultAPIBaseURL
	}
	apiBaseURL = strings.TrimRight(apiBaseURL, "/")
	if repo == "" {
		repo = DefaultRepo
	}
	if userAgent == "" {
		userAgent = "cchp/dev"
	}
	allowLoopback := isLoopbackURL(apiBaseURL)

	var transport http.RoundTripper
	if baseClient != nil && baseClient.Transport != nil {
		transport = noCredentials{next: baseClient.Transport}
	} else {
		transport = newRetryTransport(retryMax, debug)
	}

	client := &http.Client{
		Transport: traceTransport{next: transport, w: debug},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if err := validateDownloadURL(req.URL.String(), allowLoopback); err != nil {
				return err
			}
			// Never forward credentials or unexpected headers across redirects.
			req.Header.Del("Authorization")
			req.Header.Del("Proxy-Authorization")
			req.Header.Del("Cookie")
			req.Header.Set("User-Agent", userAgent)
			return nil
		},
	}
	if baseClient != nil && baseClient.Timeout > 0 {
		client.Timeout = baseClient.Timeout
	}

	return &releaseClient{
		apiBaseURL:    apiBaseURL,
		repo:          repo,
		userAgent:     userAgent,
		httpClient:    client,
		allowLoopback: allowLoopback,
		debug:         debug,
	}
}

func newRetryTransport(retryMax int, debug io.Writer) http.RoundTripper {
	base := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if retryMax <= 0 {
		return noCredentials{next: base}
	}
	rc := &retryablehttp.Client{
		HTTPClient: &http.Client{
			Transport:     noCredentials{next: base},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		RetryWaitMin: 500 * time.Millisecond,
		RetryWaitMax: 10 * time.Second,
		RetryMax:     retryMax,
		CheckRetry:   retryRead,
		Backoff:      retryablehttp.DefaultBackoff,
		ErrorHandler: retryablehttp.PassthroughErrorHandler,
	}
	if debug != nil {
		rc.RequestLogHook = func(_ retryablehttp.Logger, req *http.Request, attempt int) {
			if attempt > 0 {
				fmt.Fprintf(debug, "debug: retry %d of %s %s\n", attempt, req.Method, req.URL)
			}
		}
	}
	return &retryablehttp.RoundTripper{Client: rc}
}

func retryRead(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if err != nil {
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}
	switch resp.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true, nil
	}
	return false, nil
}

// noCredentials refuses any request carrying Authorization, Proxy-Authorization,
// Cookie, or URL userinfo. Self-update reads public GitHub Releases only and
// must never send CCHP management tokens.
type noCredentials struct{ next http.RoundTripper }

func (t noCredentials) RoundTrip(req *http.Request) (*http.Response, error) {
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if req.Header.Get(h) != "" {
			return nil, fmt.Errorf("refusing to send a %s header during self-update; cchp update never sends credentials", h)
		}
	}
	if req.URL.User != nil {
		return nil, errors.New("refusing to send URL credentials during self-update; cchp update never sends credentials")
	}
	return t.next.RoundTrip(req)
}

type traceTransport struct {
	next http.RoundTripper
	w    io.Writer
}

func (t traceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if t.w == nil {
		return resp, err
	}
	if err != nil {
		fmt.Fprintf(t.w, "debug: %s %s -> %s\n", req.Method, req.URL, redact.String(err.Error()))
		return resp, err
	}
	fmt.Fprintf(t.w, "debug: %s %s -> %d\n", req.Method, req.URL, resp.StatusCode)
	return resp, nil
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateDownloadURL(raw string, allowLoopback bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid release download URL %q", raw)
	}
	if u.User != nil {
		return errors.New("release download URL must not contain credentials")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && allowLoopback && isLoopbackURL(raw) {
		return nil
	}
	return fmt.Errorf("refusing insecure non-HTTPS download URL %q", raw)
}

func (c *releaseClient) latestReleaseURL() string {
	return fmt.Sprintf("%s/repos/%s/releases/latest", c.apiBaseURL, c.repo)
}

func (c *releaseClient) tagReleaseURL(tag string) string {
	return fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.apiBaseURL, c.repo, url.PathEscape(tag))
}

func (c *releaseClient) fetchRelease(ctx context.Context, targetTag string) (*Release, error) {
	endpoint := c.latestReleaseURL()
	if targetTag != "" {
		endpoint = c.tagReleaseURL(targetTag)
	}
	body, err := c.getBytes(ctx, endpoint, acceptGitHubJSON, maxMetadataBytes)
	if err != nil {
		return nil, err
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, contract.Fail(contract.KindServer, contract.ExitServer, "GitHub Releases returned invalid JSON: %v", err)
	}
	if rel.Draft {
		return nil, contract.Fail(contract.KindNotFound, contract.ExitNotFound, "Release %q is a draft and cannot be installed.", rel.TagName)
	}
	norm, ok := NormalizeVersion(rel.TagName)
	if !ok {
		return nil, contract.Fail(contract.KindServer, contract.ExitServer, "GitHub Releases returned an invalid release tag %q.", rel.TagName)
	}
	rel.TagName = norm
	return &rel, nil
}

func (c *releaseClient) getBytes(ctx context.Context, src, accept string, maxBytes int64) ([]byte, error) {
	if err := validateDownloadURL(src, c.allowLoopback); err != nil {
		return nil, contract.Fail(contract.KindServer, contract.ExitServer, "%v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, contract.Fail(contract.KindInternal, contract.ExitInternal, "building release request: %v", err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, transportFailure(src, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, httpFailure(src, resp.StatusCode, resp.Header, errBody)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, transportFailure(src, err)
	}
	if int64(len(body)) > maxBytes {
		return nil, contract.Fail(contract.KindServer, contract.ExitServer, "Response from %s exceeds the %d MiB limit.", src, maxBytes>>20)
	}
	return body, nil
}

func (c *releaseClient) downloadFile(ctx context.Context, src, dstPath string, maxBytes int64) (int64, error) {
	if err := validateDownloadURL(src, c.allowLoopback); err != nil {
		return 0, contract.Fail(contract.KindServer, contract.ExitServer, "%v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return 0, contract.Fail(contract.KindInternal, contract.ExitInternal, "building download request: %v", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, transportFailure(src, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return 0, httpFailure(src, resp.StatusCode, resp.Header, errBody)
	}

	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, contract.Fail(contract.KindInternal, contract.ExitInternal, "creating temporary download file: %v", err)
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(dstPath)
		return 0, transportFailure(src, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(dstPath)
		return 0, contract.Fail(contract.KindInternal, contract.ExitInternal, "saving downloaded archive: %v", closeErr)
	}
	if n > maxBytes {
		_ = os.Remove(dstPath)
		return 0, contract.Fail(contract.KindServer, contract.ExitServer, "Release archive from %s exceeds the %d MiB limit.", src, maxBytes>>20)
	}
	return n, nil
}

func transportFailure(src string, err error) *contract.Failure {
	host := src
	if u, perr := url.Parse(src); perr == nil && u.Host != "" {
		host = u.Host
	}
	msg := redact.String(err.Error())
	var dnsErr *net.DNSError
	var opErr *net.OpError
	var certErr *tls.CertificateVerificationError
	switch {
	case errors.As(err, &certErr):
		return contract.Fail(contract.KindNotSent, contract.ExitNotSent, "The update request to %s was not sent: TLS certificate verification failed: %s", host, msg)
	case errors.As(err, &dnsErr), errors.As(err, &opErr) && opErr.Op == "dial":
		f := contract.Fail(contract.KindNotSent, contract.ExitNotSent, "Could not connect to %s to check for updates: %s", host, msg)
		f.Err.Retryable = true
		return f
	case errors.Is(err, context.DeadlineExceeded):
		f := contract.Fail(contract.KindUnknownOutcome, contract.ExitUnknown, "Timed out waiting for %s before --timeout ran out.", host)
		f.Err.Retryable = true
		return f.WithHint("Try again, or allow more time with --timeout.")
	}
	f := contract.Fail(contract.KindUnknownOutcome, contract.ExitUnknown, "Could not reach %s: %s", host, msg)
	f.Err.Retryable = true
	return f
}

func httpFailure(src string, status int, header http.Header, body []byte) *contract.Failure {
	f := &contract.Failure{Status: status, RequestID: header.Get("X-GitHub-Request-Id")}
	e := &f.Err
	detail := extractGitHubErrorMessage(body)
	e.Detail = redact.String(detail)

	switch {
	case status == http.StatusNotFound:
		e.Kind, f.Exit = contract.KindNotFound, contract.ExitNotFound
		e.Message = fmt.Sprintf("Release resource not found at %s (HTTP 404).", src)
	case status == http.StatusTooManyRequests || (status == http.StatusForbidden && isGitHubRateLimited(header, detail)):
		e.Kind, f.Exit, e.Retryable = contract.KindRateLimited, contract.ExitRateLimited, true
		e.Message = "GitHub API rate limit exceeded while checking for updates."
	case status >= 500:
		e.Kind, f.Exit, e.Retryable = contract.KindServer, contract.ExitServer, true
		e.Message = fmt.Sprintf("GitHub returned HTTP %d for %s.", status, src)
	default:
		e.Kind, f.Exit = contract.KindHTTP, contract.ExitInternal
		e.Message = fmt.Sprintf("GitHub returned HTTP %d for %s.", status, src)
	}
	return f
}

func isGitHubRateLimited(header http.Header, detail string) bool {
	if header.Get("X-RateLimit-Remaining") == "0" {
		return true
	}
	return strings.Contains(strings.ToLower(detail), "rate limit")
}

func extractGitHubErrorMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil && strings.TrimSpace(payload.Message) != "" {
		return truncateText(strings.TrimSpace(payload.Message), 300)
	}
	return truncateText(strings.TrimSpace(string(body)), 300)
}

func truncateText(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}
