package cli

import (
	"context"
	"strings"

	localworker "github.com/iwen-conf/aitask-cli/internal/worker"
)

type backendSyncer struct {
	client    *Client
	projectID string
}

func (s *backendSyncer) WriteMemory(ctx context.Context, req localworker.WriteMemoryRequest) (localworker.WriteMemoryResponse, error) {
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(s.projectID)
	}
	payload, err := s.client.PostREST(ctx, "/api/projects/"+projectID+"/memory/write", map[string]any{
		"target":         req.Target,
		"title":          req.Title,
		"content":        req.Content,
		"relatedTaskId":  emptyAsNil(req.RelatedTaskID),
		"relatedEventId": emptyAsNil(req.RelatedEventID),
		"autoSync":       true,
	})
	if err != nil {
		return localworker.WriteMemoryResponse{}, err
	}
	return localworker.WriteMemoryResponse{
		URI:      mapString(payload, "uri"),
		MemoryID: fallback(mapString(payload, "memoryId"), mapString(payload, "id")),
	}, nil
}
