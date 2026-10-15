package cli

import (
	"strings"
	"testing"
)

func TestWrapCommandBreaksBeforeFlags(t *testing.T) {
	ex := "cchp api revokeProviderKey --provider-id <provider-id> --key-id <key-id> --allow-write --confirm revokeProviderKey --json"
	if got := wrapCommand(ex, 0); got != ex {
		t.Fatalf("width 0 changed the command: %q", got)
	}
	if got := wrapCommand(ex, 200); got != ex {
		t.Fatalf("a command that fits was wrapped: %q", got)
	}
	got := wrapCommand(ex, 60)
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected several lines, got %q", got)
	}
	var words []string
	for i, line := range lines {
		printed := len(line)
		if i == 0 {
			// The caller prints two spaces before the first line.
			printed += 2
		}
		if printed > 62 {
			t.Errorf("line %d is too long: %q", i, line)
		}
		if i < len(lines)-1 && !strings.HasSuffix(line, " \\") {
			t.Errorf("line %d does not end with a backslash: %q", i, line)
		}
		words = append(words, strings.Fields(strings.TrimSuffix(line, " \\"))...)
	}
	if strings.Join(words, " ") != ex {
		t.Fatalf("wrapping changed the words: %q", got)
	}
	if !strings.HasPrefix(lines[1], "     --") {
		t.Fatalf("continuation lines should start with an indented flag: %q", lines[1])
	}
}
