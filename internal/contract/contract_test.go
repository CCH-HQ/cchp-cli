package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestResultIsOneLineOfJSONWithStableKeys(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, &Result{OK: true, Operation: "listUsers", Status: 200, RequestID: "r", Data: json.RawMessage(`{"a":null,"b":false,"c":0,"d":""}`),
		Pagination: &Pagination{Pages: 1, Items: 2, HasMore: true, NextCursor: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("want exactly one line: %q", out)
	}
	want := `{"schema_version":1,"ok":true,"operation":"listUsers","status":200,"request_id":"r","data":{"a":null,"b":false,"c":0,"d":""},"pagination":{"pages":1,"items":2,"has_more":true,"next_cursor":"c"}}` + "\n"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestFailureResult(t *testing.T) {
	f := Fail(KindPolicy, ExitPolicy, "needs %s", "--allow-write").WithHint("try --dry-run")
	var buf bytes.Buffer
	if err := Write(&buf, &Result{OK: false, Error: &f.Err}); err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"ok":false,"error":{"kind":"policy","message":"needs --allow-write","hint":"try --dry-run","retryable":false}}` + "\n"
	if buf.String() != want {
		t.Fatalf("got %s", buf.String())
	}
	if f.Error() != "needs --allow-write" {
		t.Fatal(f.Error())
	}
}

func TestEventLine(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteEvent(&buf, &Event{Type: "event", Operation: "streamSystemHealth", Seq: 1, Event: "metrics", Data: json.RawMessage(`{"rpm":0}`)}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != `{"type":"event","operation":"streamSystemHealth","seq":1,"event":"metrics","data":{"rpm":0}}`+"\n" {
		t.Fatalf("got %s", buf.String())
	}
}

func TestExitCodesAreDistinct(t *testing.T) {
	codes := []int{ExitOK, ExitInternal, ExitUsage, ExitAuth, ExitPermission, ExitNotFound, ExitConflict, ExitInvalid,
		ExitBrowser, ExitRateLimited, ExitServer, ExitNotSent, ExitUnknown, ExitPolicy, ExitNotSignedIn}
	seen := map[int]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("exit code %d used twice", c)
		}
		seen[c] = true
	}
}
