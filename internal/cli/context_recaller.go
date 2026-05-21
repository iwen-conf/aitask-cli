package cli

import (
	"context"
)

type backendRecaller struct {
	client    *Client
	projectID string
}

func (r *backendRecaller) Recall(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
