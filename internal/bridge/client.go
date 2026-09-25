package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kelvinzer0/linux-agent/internal/protocol"
	"github.com/kelvinzer0/linux-agent/internal/tools"
)

type Config struct {
	BridgeURL string
	Room      string
}

type NewRoomResponse struct {
	Room         string `json:"room"`
	ExtensionURL string `json:"extension_url"`
	McpURL       string `json:"mcp_url"`
	HealthURL    string `json:"health_url"`
}

type Client struct {
	cfg      Config
	registry *tools.Registry
	ws       *websocket.Conn
	wsMu     sync.Mutex
	room     string
	stopChan chan struct{}
}

func NewClient(cfg Config, registry *tools.Registry) *Client {
	return &Client{
		cfg:      cfg,
		registry: registry,
		room:     cfg.Room,
		stopChan: make(chan struct{}),
	}
}

func (c *Client) Room() string {
	return c.room
}

func (c *Client) McpURL() string {
	if c.room == "" {
		return ""
	}
	base := strings.TrimRight(c.cfg.BridgeURL, "/")
	return fmt.Sprintf("%s/mcp?room=%s", base, c.room)
}

func (c *Client) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopChan:
			return nil
		default:
		}

		err := c.connectAndServe(ctx)
		if err != nil {
			log.Printf("[MCP-Bridge] Connection ended: %v. Reconnecting in 3 seconds...", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopChan:
			return nil
		case <-time.After(3 * time.Second):
		}
	}
}

func (c *Client) Stop() {
	close(c.stopChan)
	c.wsMu.Lock()
	if c.ws != nil {
		_ = c.ws.Close()
	}
	c.wsMu.Unlock()
}

func (c *Client) connectAndServe(ctx context.Context) error {
	wsURL := ""

	if c.room == "" {
		log.Printf("[MCP-Bridge] Allocating new room from %s/new ...", c.cfg.BridgeURL)
		newResp, err := c.allocateRoom()
		if err != nil {
			return fmt.Errorf("failed allocating room: %w", err)
		}
		c.room = newResp.Room
		wsURL = newResp.ExtensionURL
		log.Printf("[MCP-Bridge] New room allocated: %s", c.room)
		log.Printf("[MCP-Bridge] MCP URL for clients: %s", c.McpURL())
	} else {
		baseURL := strings.TrimRight(c.cfg.BridgeURL, "/")
		baseURL = strings.Replace(baseURL, "https://", "wss://", 1)
		baseURL = strings.Replace(baseURL, "http://", "ws://", 1)
		wsURL = fmt.Sprintf("%s/ws/extension?room=%s", baseURL, url.QueryEscape(c.room))
	}

	log.Printf("[MCP-Bridge] Connecting to WebSocket: %s", wsURL)

	header := make(http.Header)
	header.Set("User-Agent", "linux-agent/1.0.0")

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("dial failed with status %d: %s (%w)", resp.StatusCode, string(body), err)
		}
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	c.wsMu.Lock()
	c.ws = conn
	c.wsMu.Unlock()

	log.Printf("[MCP-Bridge] ✅ Connected successfully! Registering %d tools...", len(c.registry.GetDefinitions()))

	// Register tools immediately
	if err := c.sendRegisterTools(); err != nil {
		return fmt.Errorf("failed registering tools: %w", err)
	}
	log.Printf("[MCP-Bridge] ✅ Tools registered to room %s", c.room)

	// Keepalive ping ticker (every 25 seconds)
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopChan:
				return
			case <-ticker.C:
				_ = c.sendJSON(map[string]string{"type": "pong"})
			}
		}
	}()

	// Message loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}

		go c.handleBridgeMessage(ctx, message)
	}
}

func (c *Client) handleBridgeMessage(ctx context.Context, data []byte) {
	var generic map[string]interface{}
	if err := json.Unmarshal(data, &generic); err != nil {
		log.Printf("[MCP-Bridge] Failed parsing message: %v", err)
		return
	}

	msgType, _ := generic["type"].(string)

	switch msgType {
	case "ping":
		_ = c.sendJSON(map[string]string{"type": "pong"})

	case "callTool":
		var callMsg protocol.CallToolMessage
		if err := json.Unmarshal(data, &callMsg); err != nil {
			log.Printf("[MCP-Bridge] Malformed callTool message: %v", err)
			return
		}

		log.Printf("[MCP-Bridge] ⚙️ Executing tool: %s (callId: %s)", callMsg.Name, callMsg.CallID)
		start := time.Now()

		result, err := c.registry.Execute(ctx, callMsg.Name, callMsg.Params)
		if err != nil {
			result = protocol.ErrorResult(fmt.Sprintf("Execution error: %v", err))
		}

		log.Printf("[MCP-Bridge] 🏁 Finished %s in %v (isError: %v)", callMsg.Name, time.Since(start), result.IsError)

		response := protocol.ExtensionMessage{
			Type:   "toolResult",
			CallID: callMsg.CallID,
			Result: &result,
		}

		if err := c.sendJSON(response); err != nil {
			log.Printf("[MCP-Bridge] Failed sending toolResult: %v", err)
		}
	}
}

func (c *Client) sendRegisterTools() error {
	msg := protocol.ExtensionMessage{
		Type:  "registerTools",
		Tools: c.registry.GetDefinitions(),
	}
	return c.sendJSON(msg)
}

func (c *Client) sendJSON(v interface{}) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()

	if c.ws == nil {
		return errors.New("websocket is nil")
	}

	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteJSON(v)
}

func (c *Client) allocateRoom() (*NewRoomResponse, error) {
	base := strings.TrimRight(c.cfg.BridgeURL, "/")
	resp, err := http.Get(base + "/new")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res NewRoomResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	return &res, nil
}
