package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kelvinzer0/linux-agent/internal/bridge"
	"github.com/kelvinzer0/linux-agent/internal/stdio"
	"github.com/kelvinzer0/linux-agent/internal/tools"
)

const (
	Version = "1.0.0"
)

func main() {
	var (
		bridgeURL = flag.String("bridge", getEnv("MCP_BRIDGE_URL", "https://public-mcp-bridge.warunglakku.com"), "MCP Bridge URL")
		room      = flag.String("room", getEnv("MCP_ROOM", ""), "Room ID (if empty, generates a new room via /new)")
		useStdio  = flag.Bool("stdio", false, "Run in stdio mode (direct JSON-RPC over stdin/stdout)")
		showVer   = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("linux-agent v%s\n", Version)
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

	cfg := bridge.Config{
		BridgeURL: *bridgeURL,
		Room:      *room,
	}

	client := bridge.NewClient(cfg, registry)

	go func() {
		<-ctx.Done()
		log.Println("[linux-agent] Shutting down...")
		client.Stop()
	}()

	if err := client.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("[linux-agent] Fatal error: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
