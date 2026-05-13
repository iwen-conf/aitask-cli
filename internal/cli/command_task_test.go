package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskResumeAllowsMissingHandoffFlag(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aitask"), 0o755); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	projectDoc := "# AI Task Project\nproject_id: prj_1\nproject_name: Demo\nopenviking_root: viking://aitask/projects/prj_1\nroom_enabled: true\n"
	if err := os.WriteFile(filepath.Join(root, ".aitask", "project.md"), []byte(projectDoc), 0o644); err != nil {
		t.Fatalf("write project doc: %v", err)
	}
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir() error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousDir)
	})

	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if auth := r.Header.Get("Authorization"); auth != "Bearer tok-codex" {
			t.Fatalf("Authorization = %q, want bearer token", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"taskId":"task_1","status":"running","activeRunId":"run_recover_1"}`))
	}))
	defer server.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv(tokenStoreFileModeEnv, "file")
	store, err := NewTokenStore()
	if err != nil {
		t.Fatalf("NewTokenStore() error: %v", err)
	}
	if err := store.Save(server.URL, DefaultProfileName, "tok-codex"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")

	err = app.Execute([]string{"--server", server.URL, "--format", "json", "task", "resume", "task_1", "--run", "run_recover_1"})
	if err != nil {
		t.Fatalf("Execute(task resume) error: %v", err)
	}
	if gotPath != "/api/projects/prj_1/tasks/task_1/resume" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["runId"] != "run_recover_1" {
		t.Fatalf("runId body = %#v", gotBody)
	}
	if handoff, ok := gotBody["handoffId"].(string); !ok || handoff != "" {
		t.Fatalf("handoffId body = %#v, want empty string", gotBody["handoffId"])
	}
	if !strings.Contains(stdout.String(), `"activeRunId"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestTaskCreateForwardsStructuredFields(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aitask"), 0o755); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	projectDoc := "# AI Task Project\nproject_id: prj_1\nproject_name: Demo\nopenviking_root: viking://aitask/projects/prj_1\nroom_enabled: true\n"
	if err := os.WriteFile(filepath.Join(root, ".aitask", "project.md"), []byte(projectDoc), 0o644); err != nil {
		t.Fatalf("write project doc: %v", err)
	}
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir() error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previousDir)
	})

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/prj_1/tasks" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"taskId":"task_new","status":"planned"}`))
	}))
	defer server.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv(tokenStoreFileModeEnv, "file")
	store, err := NewTokenStore()
	if err != nil {
		t.Fatalf("NewTokenStore() error: %v", err)
	}
	if err := store.Save(server.URL, DefaultProfileName, "tok-codex"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")

	err = app.Execute([]string{
		"--server", server.URL,
		"--format", "json",
		"task", "create",
		"--title", "搭建 WebSocket Pub/Sub",
		"--goal", "前端能订阅项目内的实时事件流",
		"--inputs", "现有 SSE 轮询代码",
		"--constraints", "不允许新增第三方 WebSocket 库",
		"--output-contract", "Handler + 单元测试",
	})
	if err != nil {
		t.Fatalf("Execute(task create) error: %v", err)
	}
	if gotBody["goal"] != "前端能订阅项目内的实时事件流" {
		t.Fatalf("goal body = %#v", gotBody["goal"])
	}
	if gotBody["inputs"] != "现有 SSE 轮询代码" {
		t.Fatalf("inputs body = %#v", gotBody["inputs"])
	}
	if gotBody["constraints"] != "不允许新增第三方 WebSocket 库" {
		t.Fatalf("constraints body = %#v", gotBody["constraints"])
	}
	if gotBody["outputContract"] != "Handler + 单元测试" {
		t.Fatalf("outputContract body = %#v", gotBody["outputContract"])
	}
	if gotBody["title"] != "搭建 WebSocket Pub/Sub" {
		t.Fatalf("title body = %#v", gotBody["title"])
	}
}
