package ids

import (
	"strings"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestNewIncludesPrefixAndValidULID(t *testing.T) {
	prefixes := []string{
		PrefixProject,
		PrefixSession,
		PrefixAgent,
		PrefixRun,
		PrefixTask,
		PrefixDelegation,
		PrefixRoom,
		PrefixMessage,
		PrefixArtifact,
		PrefixHandoff,
	}

	for _, prefix := range prefixes {
		id := New(prefix)
		head, tail, ok := strings.Cut(id, "_")
		if !ok {
			t.Fatalf("New(%q) = %q, want prefix and ULID separated by '_'", prefix, id)
		}
		if head != prefix {
			t.Fatalf("New(%q) prefix = %q, want %q", prefix, head, prefix)
		}
		if _, err := ulid.Parse(tail); err != nil {
			t.Fatalf("New(%q) ULID part %q parse error: %v", prefix, tail, err)
		}
	}
}

func TestNewConcurrentUniqueForTenThousand(t *testing.T) {
	const total = 10000

	ids := make(chan string, total)
	var wg sync.WaitGroup
	wg.Add(total)

	for range total {
		go func() {
			defer wg.Done()
			ids <- New(PrefixTask)
		}()
	}

	wg.Wait()
	close(ids)

	seen := make(map[string]struct{}, total)
	for id := range ids {
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = struct{}{}
	}

	if len(seen) != total {
		t.Fatalf("unique ids = %d, want %d", len(seen), total)
	}
}

func TestNewWithoutPrefixReturnsBareULID(t *testing.T) {
	id := New("")
	if strings.Contains(id, "_") {
		t.Fatalf("New(\"\") = %q, want bare ULID without prefix", id)
	}
	if _, err := ulid.Parse(id); err != nil {
		t.Fatalf("New(\"\") ULID parse error: %v", err)
	}
}
