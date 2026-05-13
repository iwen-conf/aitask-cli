package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	AITaskDirName  = ".aitask"
	ProjectFileRel = ".aitask/project.md"
)

type ProjectConfig struct {
	RootDir               string
	AITaskDir             string
	SourceFile            string
	ProjectID             string
	ProjectName           string
	OpenVikingRoot        string
	OpenVikingNamespace   string
	OpenVikingWorkspaceID string
	RoomEnabled           bool
}

type ProjectDocValues struct {
	ProjectID             string
	ProjectName           string
	OpenVikingRoot        string
	OpenVikingNamespace   string
	OpenVikingWorkspaceID string
	RoomEnabled           bool
}

type BoundProject struct {
	ProjectID string
	FilePath  string
}

func FindProjectFile(startDir string) (string, error) {
	start := strings.TrimSpace(startDir)
	if start == "" {
		return "", errors.New("empty start directory")
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	for {
		candidate := filepath.Join(abs, AITaskDirName, "project.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	return "", fmt.Errorf("cannot find %s from %s upward", ProjectFileRel, start)
}

func LoadProjectConfig(startDir string) (ProjectConfig, error) {
	file, err := FindProjectFile(startDir)
	if err != nil {
		return ProjectConfig{}, err
	}
	return LoadProjectConfigFromFile(file)
}

func LoadProjectConfigFromFile(filePath string) (ProjectConfig, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ProjectConfig{}, err
	}
	fields := parseProjectKV(string(content))
	projectID := strings.TrimSpace(fields["project_id"])
	if projectID == "" {
		return ProjectConfig{}, fmt.Errorf("%s missing project_id", filePath)
	}
	roomEnabled := true
	if raw := strings.TrimSpace(fields["room_enabled"]); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return ProjectConfig{}, fmt.Errorf("%s has invalid room_enabled: %w", filePath, err)
		}
		roomEnabled = parsed
	}
	rootDir := filepath.Dir(filepath.Dir(filePath))
	return ProjectConfig{
		RootDir:               rootDir,
		AITaskDir:             filepath.Join(rootDir, AITaskDirName),
		SourceFile:            filePath,
		ProjectID:             projectID,
		ProjectName:           strings.TrimSpace(fields["project_name"]),
		OpenVikingRoot:        strings.TrimSpace(fields["openviking_root"]),
		OpenVikingNamespace:   strings.TrimSpace(fields["openviking_namespace"]),
		OpenVikingWorkspaceID: strings.TrimSpace(fields["openviking_workspace_id"]),
		RoomEnabled:           roomEnabled,
	}, nil
}

func parseProjectKV(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "`") {
			continue
		}
		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])
		value = strings.Trim(value, "\"")
		out[key] = value
	}
	return out
}

var tokenLikePattern = regexp.MustCompile(`(?i)(agent[_-]?token|token)\s*:`)

func ProjectFileContainsToken(filePath string) (bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}
	return tokenLikePattern.Match(content), nil
}

func EnsureAITaskLayout(rootDir string) (string, error) {
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return "", errors.New("root directory cannot be empty")
	}
	aiDir := filepath.Join(root, AITaskDirName)
	dirs := []string{
		aiDir,
		filepath.Join(aiDir, "state"),
		filepath.Join(aiDir, "skills"),
		filepath.Join(aiDir, "projects"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	return aiDir, nil
}

func InitProjectFiles(rootDir string, values ProjectDocValues) ([]string, error) {
	aiDir, err := EnsureAITaskLayout(rootDir)
	if err != nil {
		return nil, err
	}
	values = normalizeProjectDocValues(values)
	if values.ProjectID == "" {
		return nil, errors.New("project_id cannot be empty")
	}

	created := []string{}
	targets := map[string]string{
		filepath.Join(aiDir, "project.md"):                       RenderProjectMarkdown(values),
		filepath.Join(aiDir, "projects", values.ProjectID+".md"): RenderProjectMarkdown(values),
		filepath.Join(aiDir, "agent.md"):                         defaultAgentMarkdown(),
		filepath.Join(aiDir, "bootstrap.md"):                     defaultBootstrapMarkdown(),
		filepath.Join(aiDir, "context.md"):                       defaultContextMarkdown(),
		filepath.Join(aiDir, "current-task.md"):                  defaultCurrentTaskMarkdown(),
		filepath.Join(aiDir, "handoff.md"):                       defaultHandoffMarkdown(),
		filepath.Join(aiDir, "result.md"):                        defaultResultMarkdown(),
		filepath.Join(aiDir, "skills", "README.md"):              defaultSkillsMarkdown(),
	}

	for path, content := range targets {
		if _, err := os.Stat(path); err == nil {
			if strings.Contains(path, "/projects/") {
				if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
					return nil, writeErr
				}
			}
			continue
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, err
		}
		created = append(created, path)
	}

	stateFiles := []string{
		stateBootstrapPB,
		stateCurrentTaskPB,
		stateTaskDelegationPB,
		stateRoomSnapshotPB,
		stateContextUsagePB,
		stateLastSyncPB,
	}
	for _, fileName := range stateFiles {
		target := filepath.Join(aiDir, "state", fileName)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := writeStateJSON(aiDir, fileName, map[string]any{}); err != nil {
			return nil, err
		}
		created = append(created, target)
	}
	sort.Strings(created)
	return created, nil
}

func BindProject(rootDir string, values ProjectDocValues) error {
	values = normalizeProjectDocValues(values)
	if values.ProjectID == "" {
		return errors.New("project_id cannot be empty")
	}
	aiDir, err := EnsureAITaskLayout(rootDir)
	if err != nil {
		return err
	}
	projectFile := filepath.Join(aiDir, "projects", values.ProjectID+".md")
	if err := os.WriteFile(projectFile, []byte(RenderProjectMarkdown(values)), 0o644); err != nil {
		return err
	}
	active := filepath.Join(aiDir, "project.md")
	if _, err := os.Stat(active); err != nil {
		if writeErr := os.WriteFile(active, []byte(RenderProjectMarkdown(values)), 0o644); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func UseProject(rootDir string, projectID string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return "", errors.New("project_id cannot be empty")
	}
	aiDir := filepath.Join(rootDir, AITaskDirName)
	source := filepath.Join(aiDir, "projects", projectID+".md")
	payload, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("project %s not bound locally: %w", projectID, err)
	}
	target := filepath.Join(aiDir, "project.md")
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

func ListBoundProjects(rootDir string) ([]BoundProject, error) {
	path := filepath.Join(rootDir, AITaskDirName, "projects")
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []BoundProject{}, nil
		}
		return nil, err
	}
	items := make([]BoundProject, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".md")
		items = append(items, BoundProject{ProjectID: id, FilePath: filepath.Join(path, entry.Name())})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ProjectID < items[j].ProjectID })
	return items, nil
}

func normalizeProjectDocValues(values ProjectDocValues) ProjectDocValues {
	values.ProjectID = strings.TrimSpace(values.ProjectID)
	values.ProjectName = strings.TrimSpace(values.ProjectName)
	values.OpenVikingRoot = strings.TrimSpace(values.OpenVikingRoot)
	values.OpenVikingNamespace = strings.TrimSpace(values.OpenVikingNamespace)
	values.OpenVikingWorkspaceID = strings.TrimSpace(values.OpenVikingWorkspaceID)
	if values.OpenVikingRoot == "" && values.ProjectID != "" {
		values.OpenVikingRoot = "viking://aitask/projects/" + values.ProjectID
	}
	return values
}

func RenderProjectMarkdown(values ProjectDocValues) string {
	values = normalizeProjectDocValues(values)
	room := "true"
	if !values.RoomEnabled {
		room = "false"
	}
	name := values.ProjectName
	if name == "" {
		name = values.ProjectID
	}
	return fmt.Sprintf(`# AI Task Project

project_id: %s
project_name: %s
openviking_root: %s
openviking_namespace: %s
openviking_workspace_id: %s
room_enabled: %s

# Redline: never store agent token in this file.
# Token must be kept in system keychain or ~/.aitask/credentials only.

## Rule

This repository is controlled by the aitask CLI.

AI agents must not rely on chat history.

Before doing any work, run:

`+"```bash"+`
aitask bootstrap
`+"```"+`

Then run:

`+"```bash"+`
aitask task current
`+"```"+`

If there is no current task, inspect delegated tasks:

`+"```bash"+`
aitask task inbox
`+"```"+`

All task results must be submitted through:

`+"```bash"+`
aitask task submit
`+"```"+`
`, values.ProjectID, name, values.OpenVikingRoot, values.OpenVikingNamespace, values.OpenVikingWorkspaceID, room)
}

func defaultAgentMarkdown() string {
	return `# Agent Runtime Rules

The CLI token determines the actual agent identity.

Do not assume your identity from this file.
Do not pass --agent manually.
Do not start tasks that are not delegated to your Agent ID.

Allowed workflow:

1. Run aitask whoami
2. Run aitask bootstrap
3. Run aitask task current
4. If no task exists, run aitask task inbox
5. Execute only tasks delegated by the backend
6. Submit result using aitask task submit

Forbidden:

- Do not edit .aitask/state/current-task.pb manually
- Do not mark tasks as done without CLI
- Do not start tasks unless they are delegated to your Agent ID
- Do not use another Agent profile

Startup commands:

` + "```bash" + `
aitask whoami
aitask bootstrap
aitask task current
` + "```" + `

If no current task exists:

` + "```bash" + `
aitask task inbox
` + "```" + `
`
}

func defaultBootstrapMarkdown() string {
	return `# Bootstrap Protocol

You are a stateless AI Agent Run.

The project state is not in this chat.
The project state is stored in:

- Task Orchestrator backend
- OpenViking context database
- Local .aitask/project.md

Run:

` + "```bash" + `
aitask whoami
aitask bootstrap
aitask task current
` + "```" + `

If no current task exists:

` + "```bash" + `
aitask task inbox
` + "```" + `

Never create or execute work outside the delegated task.

## Context Lifecycle Rules

Before reading large context, run:

` + "```bash" + `
aitask context status
` + "```" + `

If context state is handoff_required or handoff_only, do not continue implementation.

Instead run:

` + "```bash" + `
aitask context handoff prepare
aitask context handoff submit --from .aitask/handoff.md
aitask run end --reason context_limit_handoff
` + "```" + `
`
}

func defaultContextMarkdown() string {
	return `# Context Snapshot

Run aitask bootstrap to refresh this file from backend and OpenViking refs.
`
}

func defaultCurrentTaskMarkdown() string {
	return `# Current Task

Run aitask task current to load the delegated task snapshot.
`
}

func defaultHandoffMarkdown() string {
	return `# Context Handoff

Run aitask context handoff prepare to regenerate the full handoff template before submitting.
`
}

func defaultResultMarkdown() string {
	return "# Task Result\n\n" +
		"Summarize what changed, evidence collected, and any follow-up items before running:\n\n" +
		"```bash\n" +
		"aitask task submit <task_id> --from .aitask/result.md\n" +
		"```\n"
}

func defaultSkillsMarkdown() string {
	return `# Skills Cache

Run aitask skill pull to sync project skills from OpenViking into this directory.
`
}
