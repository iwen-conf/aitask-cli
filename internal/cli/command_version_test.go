package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRootVersionFlag(t *testing.T) {
	var stdout bytes.Buffer
	app := NewApp("test-version")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")

	if err := app.Execute([]string{"--version"}); err != nil {
		t.Fatalf("Execute(--version) error: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "aitask version test-version") {
		t.Fatalf("version output = %q", got)
	}
}

func TestDaysSince(t *testing.T) {
	if got := daysSince("not-a-time"); got < 1000 {
		t.Fatalf("invalid timestamp should yield large value")
	}
	now := time.Now().UTC()
	if got := daysSince(now.Format(time.RFC3339)); got != 0 {
		t.Fatalf("today should be 0 days, got %d", got)
	}
	old := now.Add(-48 * time.Hour).Format(time.RFC3339)
	if got := daysSince(old); got < 1 {
		t.Fatalf("old timestamp should be >=1 day, got %d", got)
	}
}
