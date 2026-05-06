package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func WriteCommandError(out *os.File, err error) {
	writeCommandError(out, err)
}

func ensureParentDir(filePath string) error {
	dir := filepath.Dir(filePath)
	return os.MkdirAll(dir, 0o755)
}

func writeTextFile(filePath string, content string) error {
	if err := ensureParentDir(filePath); err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(content), 0o644)
}

func readTextFile(filePath string) (string, error) {
	payload, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func writeProtoFile(filePath string, msg proto.Message) error {
	if msg == nil {
		return fmt.Errorf("nil proto message")
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	if err := ensureParentDir(filePath); err != nil {
		return err
	}
	return os.WriteFile(filePath, payload, 0o644)
}

func readProtoFile(filePath string) ([]byte, error) {
	payload, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	msg := &structpb.Struct{}
	if err := proto.Unmarshal(payload, msg); err != nil {
		// 损坏缓存按可恢复策略处理：删除并返回明确错误，调用方可触发重建。
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("state cache corrupted and removed: %w", err)
	}
	raw, err := protojson.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func prettyJSON(value any) string {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(payload)
}

func asMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	item, ok := value.(map[string]any)
	if ok {
		return item
	}
	return map[string]any{}
}

func asSlice(value any) []any {
	if value == nil {
		return []any{}
	}
	items, ok := value.([]any)
	if ok {
		return items
	}
	return []any{}
}

func mapString(value map[string]any, key string) string {
	raw, ok := value[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
}

func mapInt(value map[string]any, key string) int {
	raw, ok := value[key]
	if !ok || raw == nil {
		return 0
	}
	switch typed := raw.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func mapBool(value map[string]any, key string) bool {
	raw, ok := value[key]
	if !ok || raw == nil {
		return false
	}
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		v := strings.ToLower(strings.TrimSpace(typed))
		return v == "true" || v == "1" || v == "yes"
	default:
		return false
	}
}

func mapFloat64(value map[string]any, key string) float64 {
	raw, ok := value[key]
	if !ok || raw == nil {
		return 0
	}
	switch typed := raw.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	default:
		return 0
	}
}
