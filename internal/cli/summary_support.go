package cli

import (
	"context"
	"database/sql"
	"fmt"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

type summaryRow struct {
	ID            string `json:"id"`
	Scope         string `json:"scope"`
	ScopeID       string `json:"scopeId"`
	Summary       string `json:"summary"`
	SourceEventID string `json:"sourceEventId,omitempty"`
	UpdatedAt     string `json:"updatedAt"`
	MemoryID      string `json:"memoryId,omitempty"`
}

func readSummaryRow(ctx context.Context, scope string, scopeID string) (summaryRow, error) {
	db, closeDB, err := localstate.Open(ctx)
	if err != nil {
		return summaryRow{}, err
	}
	defer closeDB()
	if err := localstate.Migrate(ctx, db); err != nil {
		return summaryRow{}, err
	}
	var row summaryRow
	var sourceEventID, memoryID sql.NullString
	err = db.QueryRowContext(ctx, `SELECT id, scope, scope_id, COALESCE(summary, ''), source_event_id, updated_at, memory_id
FROM summaries
WHERE scope = ? AND scope_id = ?`, scope, scopeID).Scan(&row.ID, &row.Scope, &row.ScopeID, &row.Summary, &sourceEventID, &row.UpdatedAt, &memoryID)
	row.SourceEventID = sourceEventID.String
	row.MemoryID = memoryID.String
	return row, err
}

func renderSummaryFallback(ctx context.Context, env *CommandEnv, scope string, scopeID string) error {
	if scope == "thread" {
		return printNoSummary(env, scope, scopeID)
	}
	cfg, err := env.resolveProjectConfig(true)
	if err != nil {
		return err
	}
	client, _, err := env.clientWithToken(true)
	if err != nil {
		return err
	}
	payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/memory/search", map[string]string{
		"q":        "summary " + scope + ":" + scopeID,
		"refsOnly": "true",
	})
	if err != nil {
		return err
	}
	if len(asSlice(payload["items"])) == 0 {
		return printNoSummary(env, scope, scopeID)
	}
	return env.printer().Print(RenderData{Brief: fmt.Sprintf("%d summary ref(s)", len(asSlice(payload["items"]))), Prompt: renderMemorySearchPrompt(payload), JSON: payload})
}

func printNoSummary(env *CommandEnv, scope string, scopeID string) error {
	return env.printer().Print(RenderData{
		Brief:  "no summary recorded",
		Prompt: fmt.Sprintf("# Summary\n\nNo summary recorded for %s `%s`.", scope, scopeID),
		JSON:   map[string]any{"scope": scope, "scopeId": scopeID, "summary": "", "found": false},
	})
}

func renderSummaryPrompt(row summaryRow) string {
	return fmt.Sprintf("# Summary\n\nScope: `%s`\nID: `%s`\nUpdated: `%s`\n\n%s", row.Scope, row.ScopeID, row.UpdatedAt, fallback(row.Summary, "(empty)"))
}
