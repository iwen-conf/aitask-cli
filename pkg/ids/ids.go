package ids

import (
	"strings"

	"github.com/oklog/ulid/v2"
)

const (
	PrefixProject    = "prj"
	PrefixSession    = "sess"
	PrefixAgent      = "agt"
	PrefixRun        = "run"
	PrefixTask       = "task"
	PrefixDelegation = "dlg"
	PrefixRoom       = "room"
	PrefixMessage    = "msg"
	PrefixArtifact   = "art"
	PrefixHandoff    = "handoff"
)

func New(prefix string) string {
	id := ulid.Make().String()
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return id
	}

	return prefix + "_" + id
}
