package cli

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type eventsFileSink struct {
	path       string
	maxSize    int64
	maxBackups int
	compress   bool
	mu         sync.Mutex
}

type eventsNotifier struct {
	mode  string
	kinds map[string]bool
}

func newEventsEmitter(env *CommandEnv, opts eventsOptions) (*eventsEmitter, error) {
	var writers []io.Writer
	var closers []io.Closer
	if opts.stdout {
		writers = append(writers, env.app.Stdout)
	}
	if strings.TrimSpace(opts.outputFile) != "" {
		path, err := expandUserPath(opts.outputFile)
		if err != nil {
			return nil, err
		}
		fileSink := &eventsFileSink{
			path:       path,
			maxSize:    opts.rotateSize,
			maxBackups: opts.rotateBackups,
			compress:   opts.rotateCompress,
		}
		writers = append(writers, fileSink)
		closers = append(closers, fileSink)
	}
	return &eventsEmitter{
		stream:   writers,
		stderr:   env.app.Stderr,
		closer:   multiCloser(closers),
		notifier: newEventsNotifier(opts.notify, opts.notifyKinds),
	}, nil
}

func (s *eventsFileSink) Write(payload []byte) (int, error) {
	if s == nil {
		return len(payload), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return 0, err
	}
	if err := s.rotateIfNeeded(int64(len(payload))); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return file.Write(payload)
}

func (s *eventsFileSink) Close() error {
	return nil
}

func (s *eventsFileSink) rotateIfNeeded(incoming int64) error {
	if s.maxSize <= 0 {
		return nil
	}
	info, err := os.Stat(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Size()+incoming <= s.maxSize {
		return nil
	}
	if s.maxBackups <= 0 {
		return os.Remove(s.path)
	}
	for i := s.maxBackups; i >= 1; i-- {
		from := backupPath(s.path, i, s.compress)
		to := backupPath(s.path, i+1, s.compress)
		if i == s.maxBackups {
			_ = os.Remove(from)
			continue
		}
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	if s.compress {
		if err := gzipFile(s.path, backupPath(s.path, 1, true)); err != nil {
			return err
		}
		return os.Remove(s.path)
	}
	if err := os.Rename(s.path, backupPath(s.path, 1, false)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func backupPath(path string, index int, compress bool) string {
	out := path + "." + strconv.Itoa(index)
	if compress {
		out += ".gz"
	}
	return out
}

func gzipFile(source string, target string) error {
	in, err := os.Open(source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		return err
	}
	return gz.Close()
}

func multiCloser(closers []io.Closer) io.Closer {
	return closerFunc(func() error {
		var errs []error
		for _, closer := range closers {
			if closer == nil {
				continue
			}
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	})
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func newEventsNotifier(mode string, kinds []string) *eventsNotifier {
	mode = strings.TrimSpace(mode)
	if mode == "" || mode == "none" {
		return nil
	}
	set := map[string]bool{}
	for _, kind := range kinds {
		kind = strings.TrimSpace(kind)
		if kind != "" {
			set[kind] = true
		}
	}
	return &eventsNotifier{mode: mode, kinds: set}
}

func (n *eventsNotifier) Notify(event eventsOutput) error {
	if n == nil || !n.kinds[event.Kind] {
		return nil
	}
	title, body := summarizeEventNotification(event)
	if strings.TrimSpace(title) == "" {
		return nil
	}
	switch n.mode {
	case "terminal-notifier":
		return runTerminalNotifier(title, body)
	case "osascript":
		return runOSAScriptNotification(title, body)
	case "auto":
		if _, err := exec.LookPath("terminal-notifier"); err == nil {
			return runTerminalNotifier(title, body)
		}
		if runtime.GOOS == "darwin" {
			return runOSAScriptNotification(title, body)
		}
		return nil
	default:
		return nil
	}
}

func summarizeEventNotification(event eventsOutput) (string, string) {
	project := event.Project
	if project == "" && len(event.Projects) > 0 {
		project = event.Projects[0]
	}
	if len(project) > 12 {
		project = project[:12]
	}
	title := fmt.Sprintf("[aitask %s] %s", event.Kind, project)
	switch event.Kind {
	case eventKindMention:
		body := strings.TrimSpace(event.Content)
		if event.From != nil {
			if from := eventSenderLabel(*event.From); from != "" && body != "" {
				body = from + ": " + body
			}
		}
		if body == "" {
			body = "Mention received"
		}
		return title, trimEventNotification(body, 80)
	case eventKindTaskDelegated:
		name := taskTitleFromDetails(event.Details)
		if name == "" {
			name = "Task delegated"
		}
		if event.TaskID != "" {
			name += " (" + event.TaskID + ")"
		}
		return title, trimEventNotification(name, 80)
	default:
		return title, trimEventNotification(event.Reason, 80)
	}
}

func eventSenderLabel(sender eventsSender) string {
	for _, value := range []*string{sender.AgentID, sender.AgentType, sender.OperatorLabel} {
		if value != nil && strings.TrimSpace(*value) != "" {
			return strings.TrimSpace(*value)
		}
	}
	return strings.TrimSpace(sender.Type)
}

func taskTitleFromDetails(details map[string]any) string {
	for _, key := range []string{"title", "taskTitle", "name"} {
		if value := mapString(details, key); value != "" {
			return value
		}
	}
	if task := asMap(details["task"]); len(task) > 0 {
		return taskTitleFromDetails(task)
	}
	return ""
}

func trimEventNotification(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

func runTerminalNotifier(title string, body string) error {
	path, err := exec.LookPath("terminal-notifier")
	if err != nil {
		return nil
	}
	return exec.Command(path, "-title", title, "-message", body, "-group", "aitask-watch").Run()
}

func runOSAScriptNotification(title string, body string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
	return exec.Command("/usr/bin/osascript", "-e", script).Run()
}

func escapeAppleScript(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

func expandUserPath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			return home, nil
		}
		return filepath.Join(home, value[2:]), nil
	}
	return value, nil
}

func parseEventsSize(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, errors.New("size cannot be empty")
	}
	lower := strings.ToLower(value)
	multiplier := int64(1)
	for _, unit := range []struct {
		suffix string
		mult   int64
	}{
		{"kib", 1024},
		{"kb", 1000},
		{"k", 1024},
		{"mib", 1024 * 1024},
		{"mb", 1000 * 1000},
		{"m", 1024 * 1024},
		{"gib", 1024 * 1024 * 1024},
		{"gb", 1000 * 1000 * 1000},
		{"g", 1024 * 1024 * 1024},
		{"b", 1},
	} {
		if strings.HasSuffix(lower, unit.suffix) {
			multiplier = unit.mult
			value = strings.TrimSpace(value[:len(value)-len(unit.suffix)])
			break
		}
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", raw)
	}
	if number <= 0 {
		return 0, errors.New("size must be positive")
	}
	return int64(number * float64(multiplier)), nil
}
