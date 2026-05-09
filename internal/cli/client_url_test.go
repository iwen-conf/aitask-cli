package cli

import "testing"

func TestClientWebSocketURLKeepsBasePath(t *testing.T) {
	client := NewClient("http://127.0.0.1:18081/base", 0, "")
	got, err := client.WebSocketURL("prj_123")
	if err != nil {
		t.Fatalf("WebSocketURL() error = %v", err)
	}
	const want = "ws://127.0.0.1:18081/base/ws/projects/prj_123/agent-room"
	if got != want {
		t.Fatalf("WebSocketURL() = %q, want %q", got, want)
	}
}

func TestClientWebSocketURLUpgradesHTTPS(t *testing.T) {
	client := NewClient("https://api.example.com/v1", 0, "")
	got, err := client.WebSocketURL("prj_abc")
	if err != nil {
		t.Fatalf("WebSocketURL() error = %v", err)
	}
	const want = "wss://api.example.com/v1/ws/projects/prj_abc/agent-room"
	if got != want {
		t.Fatalf("WebSocketURL() = %q, want %q", got, want)
	}
}
