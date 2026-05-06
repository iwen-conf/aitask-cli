package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	stateBootstrapPB      = "bootstrap.pb"
	stateCurrentTaskPB    = "current-task.pb"
	stateTaskDelegationPB = "task-delegation.pb"
	stateRoomSnapshotPB   = "room-snapshot.pb"
	stateContextUsagePB   = "context-usage.pb"
	stateLastSyncPB       = "last-sync.pb"
)

func writeStateProto(aiDir string, fileName string, msg *structpb.Struct) error {
	if strings.TrimSpace(aiDir) == "" {
		return fmt.Errorf("ai dir cannot be empty")
	}
	if msg == nil {
		msg = &structpb.Struct{Fields: map[string]*structpb.Value{}}
	}
	target := filepath.Join(aiDir, "state", fileName)
	return writeProtoFile(target, msg)
}

func writeStateJSON(aiDir string, fileName string, payload map[string]any) error {
	msg, err := structFromMap(payload)
	if err != nil {
		return err
	}
	return writeStateProto(aiDir, fileName, msg)
}

func writeLastSyncState(aiDir string, command string, status string, retriable bool) error {
	payload := map[string]any{
		"command":   strings.TrimSpace(command),
		"status":    strings.TrimSpace(status),
		"retriable": retriable,
		"syncAt":    timestamppb.Now().AsTime().UTC().Format(time.RFC3339),
	}
	return writeStateJSON(aiDir, stateLastSyncPB, payload)
}

func safeWriteLastSync(aiDir string, command string, status string, retriable bool) {
	if strings.TrimSpace(aiDir) == "" {
		return
	}
	_ = writeLastSyncState(aiDir, command, status, retriable)
}

func readStateJSON(aiDir string, fileName string) (map[string]any, error) {
	target := filepath.Join(aiDir, "state", fileName)
	raw, err := readProtoFile(target)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func structFromMap(payload map[string]any) (*structpb.Struct, error) {
	if payload == nil {
		return &structpb.Struct{Fields: map[string]*structpb.Value{}}, nil
	}
	normalized, ok := normalizeStructValue(payload).(map[string]any)
	if !ok {
		return &structpb.Struct{Fields: map[string]*structpb.Value{}}, nil
	}
	return structpb.NewStruct(normalized)
}

func normalizeStructValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalizeStructValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeStructValue(item))
		}
		return out
	case []string:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []int:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []int64:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []float64:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	case []bool:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return value
	}
}
