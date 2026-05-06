package cli

import (
	"errors"
	"testing"
)

func TestEnhanceCommandError(t *testing.T) {
	in := &APIError{Code: codeAgentTokenExpired, Message: "expired", Details: map[string]any{}}
	out := enhanceCommandError(in)
	var apiErr *APIError
	if !errors.As(out, &apiErr) {
		t.Fatalf("enhanced error should keep APIError type")
	}
	if apiErr.Message == "expired" {
		t.Fatalf("message should be replaced by human guide")
	}
	if mapString(apiErr.Details, "nextCommand") == "" {
		t.Fatalf("next command hint should exist")
	}
}
