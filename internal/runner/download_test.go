package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

const downloadTestToken = "cch_auth_live_TESTPREFIX_secretpart123"

// archiveServer serves request archive parts the way CCHP does: each part with its
// own Content-Type, and the archive listing with every part's size.
type archiveServer struct {
	parts map[string]archivePart
}

type archivePart struct {
	contentType string
	body        []byte
	// listedSize is the size the archive listing reports for the part.
	listedSize int64
	// status, when set, answers the download with this status and a problem body.
	status int
}

func (s *archiveServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+downloadTestToken {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":10003,"detail":"unauthorized"}`))
		return
	}
	const prefix = "/api/account/usage/logs/req-1/archive"
	switch {
	case r.URL.Path == prefix:
		w.Header().Set("Content-Type", "application/json")
		var listing bytes.Buffer
		listing.WriteString(`{"requestId":"req-1","available":true,"external":false,"langfuse":null,"parts":[`)
		first := true
		for name, p := range s.parts {
			if !first {
				listing.WriteString(",")
			}
			first = false
			listing.WriteString(`{"name":"` + name + `","size":` + itoa(p.listedSize) + `,"originalSize":` + itoa(p.listedSize) + `}`)
		}
		listing.WriteString(`]}`)
		_, _ = w.Write(listing.Bytes())
	case strings.HasPrefix(r.URL.Path, prefix+"/raw/"):
		// Like CCHP, the part segment is decoded once more after the URL path, so
		// client%2Fresponse.frames arrives whether the client escaped it once or twice.
		name, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, prefix+"/raw/"))
		p, ok := s.parts[name]
		if err != nil || !ok {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":10005,"detail":"archive part not found"}`))
			return
		}
		if p.status != 0 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("X-Request-Id", "rid-error")
			w.WriteHeader(p.status)
			_, _ = w.Write([]byte(`{"code":30520,"detail":"request archive read budget exhausted","request_id":"rid-error"}`))
			return
		}
		w.Header().Set("Content-Type", p.contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", `attachment; filename="req-1-part"`)
		_, _ = w.Write(p.body)
	default:
		http.NotFound(w, r)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// sseFrames is a CCHP archive response.frames part: one SSE frame per upstream event.
func sseFrames(n int) []byte {
	var b bytes.Buffer
	for i := range n {
		b.WriteString("event: response.output_text.delta\n")
		b.WriteString(`data: {"type":"response.output_text.delta","delta":"tok` + strconv.Itoa(i) + ` é"}` + "\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.Bytes()
}

func ndjsonLines(n int) []byte {
	var b bytes.Buffer
	for i := range n {
		b.WriteString(`{"seq":` + strconv.Itoa(i) + `,"text":"line"}` + "\n")
	}
	return b.Bytes()
}

func newDownloadRunner(t *testing.T, server string) *Runner {
	t.Helper()
	t.Setenv("CCHP_BASE_URL", server)
	t.Setenv("CCHP_TOKEN", downloadTestToken)
	t.Setenv("CCHP_TOKEN_FILE", "")
	dir := t.TempDir()
	return New(&auth.Store{Dirs: auth.Dirs{Config: filepath.Join(dir, "config"), Cache: filepath.Join(dir, "cache")}, Backend: "file"}, "test")
}

// curlPart downloads the part with curl, the reference the CLI must match byte for byte.
func curlPart(t *testing.T, server, part string) []byte {
	t.Helper()
	cmd := exec.Command("curl", "-sS", "--fail", "-H", "Authorization: Bearer "+downloadTestToken,
		server+"/api/account/usage/logs/req-1/archive/raw/"+part)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("curl %s: %v: %s", part, err, stderr.String())
	}
	return out.Bytes()
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func download(t *testing.T, r *Runner, part, output string) (*contract.Result, error) {
	t.Helper()
	return r.Run(context.Background(), Options{
		Op:     mustOp(t, "getUsageLogArchiveRaw"),
		Params: map[string][]string{"request-id": {"req-1"}, "part": {part}},
		Output: output,
		Stdout: &bytes.Buffer{},
	})
}

// Every Content-Type is saved byte for byte: the saved file, the reported length
// and the reported sha256 all match what curl receives.
func TestDownloadSavesRawBytesForEveryContentType(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}
	srv := &archiveServer{parts: map[string]archivePart{
		"client/request.body":        {contentType: "application/json", body: []byte(`{"model":"gpt-4o-mini","input":"hi é","n":12345678901234567890}` + "\n")},
		"client/response.frames":     {contentType: "text/event-stream", body: sseFrames(4000)},
		"attempt/0/response.frames":  {contentType: "text/event-stream; charset=utf-8", body: sseFrames(1200)},
		"attempt/0/response.body":    {contentType: "application/octet-stream", body: bytes.Repeat([]byte{0, 1, 2, 255, '\n', '\r'}, 50000)},
		"attempt/1/response.ndjson":  {contentType: "application/x-ndjson", body: ndjsonLines(3000)},
		"attempt/1/response.jsonl":   {contentType: "application/jsonl", body: ndjsonLines(10)},
		"client/request.body.plain":  {contentType: "text/plain; charset=utf-8", body: []byte("plain text\nwith lines\n")},
		"client/response.frames.gap": {contentType: "text/event-stream", body: []byte(": keepalive\n\n: keepalive\n\n")},
	}}
	for name, p := range srv.parts {
		p.listedSize = int64(len(p.body))
		srv.parts[name] = p
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()
	r := newDownloadRunner(t, ts.URL)
	for name := range srv.parts {
		part := strings.ReplaceAll(name, "/", "%2F")
		want := curlPart(t, ts.URL, part)
		if !bytes.Equal(want, srv.parts[name].body) {
			t.Fatalf("%s: curl received %d bytes, the server sent %d", name, len(want), len(srv.parts[name].body))
		}
		out := filepath.Join(t.TempDir(), "part.out")
		res, err := download(t, r, part, out)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if !res.OK || res.File == nil {
			t.Fatalf("%s: result %+v", name, res)
		}
		if int64(len(got)) != int64(len(want)) || res.File.Bytes != int64(len(want)) {
			t.Fatalf("%s: saved %d bytes, reported %d, curl %d", name, len(got), res.File.Bytes, len(want))
		}
		if sha(got) != sha(want) || res.File.SHA256 != sha(want) {
			t.Fatalf("%s: saved sha256 %s, reported %s, curl %s", name, sha(got), res.File.SHA256, sha(want))
		}
		if res.File.ContentType != srv.parts[name].contentType {
			t.Fatalf("%s: content type %q, want %q", name, res.File.ContentType, srv.parts[name].contentType)
		}
	}
}

// --output - writes the raw bytes to standard output.
func TestDownloadToStdoutWritesRawBytes(t *testing.T) {
	body := sseFrames(50)
	ts := httptest.NewServer(&archiveServer{parts: map[string]archivePart{
		"client/response.frames": {contentType: "text/event-stream", body: body, listedSize: int64(len(body))},
	}})
	defer ts.Close()
	r := newDownloadRunner(t, ts.URL)
	var stdout bytes.Buffer
	res, err := r.Run(context.Background(), Options{
		Op:     mustOp(t, "getUsageLogArchiveRaw"),
		Params: map[string][]string{"request-id": {"req-1"}, "part": {"client%2Fresponse.frames"}},
		Output: "-",
		Stdout: &stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), body) || res.File.Bytes != int64(len(body)) || res.File.SHA256 != sha(body) {
		t.Fatalf("stdout %d bytes, reported %d, want %d", stdout.Len(), res.File.Bytes, len(body))
	}
}

// A part the archive lists as empty is saved as an empty file and reported as such.
func TestDownloadGenuinelyEmptyPart(t *testing.T) {
	ts := httptest.NewServer(&archiveServer{parts: map[string]archivePart{
		"client/request.body": {contentType: "application/json", body: nil, listedSize: 0},
	}})
	defer ts.Close()
	r := newDownloadRunner(t, ts.URL)
	out := filepath.Join(t.TempDir(), "empty.out")
	res, err := download(t, r, "client%2Frequest.body", out)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || len(got) != 0 || res.File.Bytes != 0 || res.File.SHA256 != sha(nil) {
		t.Fatalf("result %+v, saved %d bytes", res, len(got))
	}
}

// A part the archive lists as not empty but the server sends empty fails: the
// saved file is incomplete and the result is never ok.
func TestDownloadEmptyBodyOfListedPartFails(t *testing.T) {
	ts := httptest.NewServer(&archiveServer{parts: map[string]archivePart{
		"client/response.frames": {contentType: "text/event-stream", body: nil, listedSize: 345678},
	}})
	defer ts.Close()
	r := newDownloadRunner(t, ts.URL)
	res, err := download(t, r, "client%2Fresponse.frames", filepath.Join(t.TempDir(), "frames.out"))
	if res != nil {
		t.Fatalf("result %+v, want a failure", res)
	}
	var f *contract.Failure
	if !errors.As(err, &f) || f.Err.Kind != contract.KindServer || f.Exit != contract.ExitServer ||
		!strings.Contains(f.Err.Message, "345678 bytes") {
		t.Fatalf("error %v, want a server failure naming the listed size", err)
	}
}

// An error response is never reported as ok and writes no file.
func TestDownloadErrorResponseFails(t *testing.T) {
	ts := httptest.NewServer(&archiveServer{parts: map[string]archivePart{
		"client/response.frames": {contentType: "text/event-stream", status: http.StatusServiceUnavailable, listedSize: 10},
	}})
	defer ts.Close()
	r := newDownloadRunner(t, ts.URL)
	out := filepath.Join(t.TempDir(), "frames.out")
	res, err := download(t, r, "client%2Fresponse.frames", out)
	if res != nil {
		t.Fatalf("result %+v, want a failure", res)
	}
	var f *contract.Failure
	if !errors.As(err, &f) || f.Status != http.StatusServiceUnavailable || f.Exit != contract.ExitServer || f.RequestID != "rid-error" ||
		!strings.Contains(f.Err.Detail, "read budget exhausted") {
		t.Fatalf("error %#v, want the 503 problem", err)
	}
	if _, statErr := os.Stat(out); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("an error response left a file behind: %v", statErr)
	}
}
