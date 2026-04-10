// Command agentic-coop-db-mcp is a standalone MCP server that proxies tool
// calls to the Agentic Coop DB HTTP gateway.
//
// It reads the gateway URL and API key from environment variables, starts an
// MCP server on stdio, and translates tool calls into authenticated HTTP
// requests. Every call traverses the gateway's full middleware chain (auth,
// rate limiting, tenant isolation, SQL validation, audit).
//
// Usage:
//
//	AGENTCOOPDB_GATEWAY_URL=https://db.example.com \
//	AGENTCOOPDB_API_KEY=acd_live_<id>_<secret>     \
//	  agentic-coop-db-mcp
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	mcppkg "github.com/fheinfling/agentic-coop-db/internal/mcp"
	"github.com/fheinfling/agentic-coop-db/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: agentic-coop-db-mcp [flags]

MCP server for Agentic Coop DB. Connects to the HTTP gateway and exposes
database operations as MCP tools (sql_execute, rpc_call, list_tables,
describe_table, vector_search, vector_upsert, whoami, health).

Transport: stdio (stdin/stdout)

env:
  AGENTCOOPDB_GATEWAY_URL      base URL of the gateway (e.g. https://db.example.com)
  AGENTCOOPDB_API_KEY           API key (acd_<env>_<id>_<secret>)
  AGENTCOOPDB_API_KEY_FILE      file containing the API key (docker secret pattern)

flags:`)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		info := version.Get()
		fmt.Fprintf(os.Stderr, "agentic-coop-db-mcp %s (%s, %s)\n", info.Version, info.Commit, info.BuildDate)
		os.Exit(0)
	}

	// All logging goes to stderr — stdout is the MCP transport.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	gatewayURL := os.Getenv("AGENTCOOPDB_GATEWAY_URL")
	if gatewayURL == "" {
		fmt.Fprintln(os.Stderr, "error: AGENTCOOPDB_GATEWAY_URL is required")
		os.Exit(2)
	}

	apiKey := os.Getenv("AGENTCOOPDB_API_KEY")
	if apiKey == "" {
		if path := os.Getenv("AGENTCOOPDB_API_KEY_FILE"); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: read AGENTCOOPDB_API_KEY_FILE: %v\n", err)
				os.Exit(2)
			}
			apiKey = strings.TrimSpace(string(b))
		}
	}
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: AGENTCOOPDB_API_KEY or AGENTCOOPDB_API_KEY_FILE is required")
		os.Exit(2)
	}

	client := mcppkg.NewClient(mcppkg.ClientConfig{
		GatewayURL: gatewayURL,
		APIKey:     apiKey,
	})

	srv := mcppkg.NewServer(client)

	info := version.Get()
	logger.Info("starting MCP server",
		"version", info.Version,
		"gateway", gatewayURL,
		"transport", "stdio",
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := mcppkg.RunStdio(ctx, srv); err != nil {
		logger.Error("mcp server error", "err", err)
		os.Exit(1)
	}
}
