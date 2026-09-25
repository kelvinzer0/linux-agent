package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kelvinzer0/linux-agent/internal/bridge"
	"github.com/kelvinzer0/linux-agent/internal/stdio"
	"github.com/kelvinzer0/linux-agent/internal/tools"
)

const (
	Version = "1.0.2"
)

func main() {
	// Support "linux-agent status" subcommand without flags
	if len(os.Args) > 1 && os.Args[1] == "status" {
		printStatusAndExit()
	}

	var (
		bridgeURL  = flag.String("bridge", getEnv("MCP_BRIDGE_URL", "https://public-mcp-bridge.warunglakku.com"), "MCP Bridge URL")
		room       = flag.String("room", getEnv("MCP_ROOM", ""), "Room ID (if empty, generates a new room via /new)")
		useStdio   = flag.Bool("stdio", false, "Run in stdio mode (direct JSON-RPC over stdin/stdout)")
		showVer    = flag.Bool("version", false, "Show version and exit")
		showStatus = flag.Bool("status", false, "Check daemon status and print current MCP URL")
		doUpdate   = flag.Bool("update", false, "Check and perform self-update from GitHub Releases")
		autoUpdate = flag.Bool("auto-update", getEnvBool("AUTO_UPDATE", true), "Enable background periodic auto-update checks")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("linux-agent v%s\n", Version)
		os.Exit(0)
	}

	if *showStatus {
		printStatusAndExit()
	}

	if *doUpdate {
		if err := CheckAndUpdate(true); err != nil {
			log.Fatalf("[updater] Update error: %v", err)
		}
		os.Exit(0)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	registry := tools.NewRegistry()

	if *useStdio {
		log.Println("[linux-agent] Starting in stdio mode...")
		if err := stdio.ServeStdio(ctx, registry); err != nil {
			log.Fatalf("[linux-agent] Stdio server error: %v", err)
		}
		return
	}

	log.Printf("[linux-agent] v%s starting...", Version)
	log.Printf("[linux-agent] Registered tools: %d", len(registry.GetDefinitions()))
	for _, t := range registry.GetDefinitions() {
		log.Printf("  • %-20s - %s", t.Name, t.Description)
	}

	// Periodic auto-update in background
	if *autoUpdate {
		go func() {
			// Check after 1 hour, then every 24 hours
			time.Sleep(1 * time.Hour)
			_ = CheckAndUpdate(false)

			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					_ = CheckAndUpdate(false)
				}
			}
		}()
	}

	cfg := bridge.Config{
		BridgeURL: *bridgeURL,
		Room:      *room,
	}

	client := bridge.NewClient(cfg, registry, Version)

	go func() {
		<-ctx.Done()
		log.Println("[linux-agent] Shutting down...")
		client.Stop()
	}()

	if err := client.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("[linux-agent] Fatal error: %v", err)
	}
}

func printStatusAndExit() {
	st, running, err := bridge.ReadDaemonStatus()
	if err != nil {
		fmt.Printf("⚠️ Error checking status: %v\n", err)
		os.Exit(1)
	}

	if !running || st == nil {
		fmt.Println("==================================================")
		fmt.Println("🔴 linux-agent is STOPPED / NOT RUNNING")
		fmt.Println("==================================================")
		fmt.Println("Start it with:")
		fmt.Println("  sudo systemctl start linux-agent")
		fmt.Println("  or: linux-agent")
		fmt.Println("==================================================")
		os.Exit(3)
	}

	fmt.Println("==================================================")
	fmt.Printf("🟢 linux-agent is RUNNING (PID %d)\n", st.PID)
	fmt.Println("==================================================")
	fmt.Printf("🔗 MCP URL:       %s\n", st.McpURL)
	fmt.Printf("🔑 Room ID:       %s\n", st.Room)
	fmt.Printf("🌐 Bridge URL:    %s\n", st.BridgeURL)
	fmt.Printf("🔌 WebSocket:     %s\n", st.WebsocketURL)
	fmt.Printf("🛠️  Active Tools:  %d tools registered\n", st.ToolsCount)
	fmt.Printf("📦 Version:       v%s\n", st.Version)
	fmt.Println("==================================================")
	fmt.Println("💡 To connect MCP Clients (Claude, Cursor, Antigravity):")
	fmt.Printf("   URL: %s\n", st.McpURL)
	fmt.Println("==================================================")
	os.Exit(0)
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val == "1" || val == "true" || val == "TRUE" || val == "yes"
}
