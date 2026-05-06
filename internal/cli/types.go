package cli

import (
	"fmt"
	"strings"
)

type OutputFormat string

const (
	FormatBrief  OutputFormat = "brief"
	FormatPrompt OutputFormat = "prompt"
	FormatJSON   OutputFormat = "json"
	FormatProto  OutputFormat = "proto"
)

func ParseOutputFormat(raw string) (OutputFormat, error) {
	value := OutputFormat(strings.TrimSpace(strings.ToLower(raw)))
	switch value {
	case FormatBrief, FormatPrompt, FormatJSON, FormatProto:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported format %q (allowed: brief|prompt|json|proto)", raw)
	}
}

type APIError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retriable bool           `json:"retriable"`
	Details   map[string]any `json:"details"`
}

func (e *APIError) Error() string {
	if e == nil {
		return "api error"
	}
	code := strings.TrimSpace(e.Code)
	if code == "" {
		code = "INTERNAL"
	}
	if strings.TrimSpace(e.Message) == "" {
		return code
	}
	return code + ": " + e.Message
}
