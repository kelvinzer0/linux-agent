package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type DaemonStatus struct {
	Status       string    `json:"status"`
	Version      string    `json:"version"`
	PID          int       `json:"pid"`
	Room         string    `json:"room"`
	BridgeURL    string    `json:"bridge_url"`
	McpURL       string    `json:"mcp_url"`
	WebsocketURL string    `json:"websocket_url"`
	ToolsCount   int       `json:"tools_count"`
	StartedAt    time.Time `json:"started_at"`
}

func getStatusPaths() (jsonPath string, urlPath string) {
	// Standard Linux runtime dirs: /run or /var/run if root, else ~/.linux-agent
	if os.Geteuid() == 0 {
		return "/var/run/linux-agent/status.json", "/var/run/linux-agent.url"
	}
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".linux-agent")
	return filepath.Join(base, "status.json"), filepath.Join(base, "linux-agent.url")
}

func (c *Client) SaveStatus(version string) {
	jsonPath, urlPath := getStatusPaths()

	_ = os.MkdirAll(filepath.Dir(jsonPath), 0755)

	status := DaemonStatus{
		Status:       "connected",
		Version:      version,
		PID:          os.Getpid(),
		Room:         c.room,
		BridgeURL:    c.cfg.BridgeURL,
		McpURL:       c.McpURL(),
		WebsocketURL: fmt.Sprintf("%s/ws/extension?room=%s", c.cfg.BridgeURL, c.room),
		ToolsCount:   len(c.registry.GetDefinitions()),
		StartedAt:    time.Now(),
	}

	data, err := json.MarshalIndent(status, "", "  ")
	if err == nil {
		_ = os.WriteFile(jsonPath, data, 0644)
	}

	// Also write plain URL for simple grep/cat
	_ = os.WriteFile(urlPath, []byte(c.McpURL()+"\n"), 0644)
}

func (c *Client) CleanupStatus() {
	jsonPath, urlPath := getStatusPaths()
	_ = os.Remove(jsonPath)
	_ = os.Remove(urlPath)
}

// ReadDaemonStatus reads status from disk and checks if the process is still running.
func ReadDaemonStatus() (*DaemonStatus, bool, error) {
	// Check root paths first, then user home
	candidates := []string{
		"/var/run/linux-agent/status.json",
		"/run/linux-agent/status.json",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".linux-agent", "status.json"))
	}

	var data []byte
	var foundPath string
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err == nil {
			data = b
			foundPath = p
			break
		}
	}

	if data == nil {
		return nil, false, nil
	}

	var status DaemonStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, false, err
	}

	// Check if PID is still alive
	running := false
	if status.PID > 0 {
		process, err := os.FindProcess(status.PID)
		if err == nil {
			// On Unix, signal 0 checks for process existence without sending a signal
			if err := process.Signal(syscall.Signal(0)); err == nil {
				running = true
			}
		}
	}

	if !running {
		// Clean stale status
		_ = os.Remove(foundPath)
	}

	return &status, running, nil
}
