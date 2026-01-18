package proxy

import (
	"net/http"
	"testing"
)

func TestIsWebSocketUpgrade(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		wantResult bool
	}{
		{
			name: "valid websocket upgrade",
			headers: map[string]string{
				"Upgrade":    "websocket",
				"Connection": "upgrade",
			},
			wantResult: true,
		},
		{
			name: "valid websocket upgrade with keep-alive",
			headers: map[string]string{
				"Upgrade":    "websocket",
				"Connection": "keep-alive, upgrade",
			},
			wantResult: true,
		},
		{
			name: "case insensitive upgrade header",
			headers: map[string]string{
				"Upgrade":    "WebSocket",
				"Connection": "Upgrade",
			},
			wantResult: true,
		},
		{
			name: "case insensitive connection header",
			headers: map[string]string{
				"Upgrade":    "websocket",
				"Connection": "UPGRADE",
			},
			wantResult: true,
		},
		{
			name: "missing upgrade header",
			headers: map[string]string{
				"Connection": "upgrade",
			},
			wantResult: false,
		},
		{
			name: "missing connection header",
			headers: map[string]string{
				"Upgrade": "websocket",
			},
			wantResult: false,
		},
		{
			name: "wrong upgrade value",
			headers: map[string]string{
				"Upgrade":    "h2c",
				"Connection": "upgrade",
			},
			wantResult: false,
		},
		{
			name: "connection without upgrade",
			headers: map[string]string{
				"Upgrade":    "websocket",
				"Connection": "keep-alive",
			},
			wantResult: false,
		},
		{
			name:       "no headers",
			headers:    map[string]string{},
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "http://example.com/ws", nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			got := isWebSocketUpgrade(req)
			if got != tt.wantResult {
				t.Errorf("isWebSocketUpgrade() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}
