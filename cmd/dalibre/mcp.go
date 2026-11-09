package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/egoist/mygo"

	"dalibre/internal/mcp"
	"dalibre/internal/settings"
)

// serveMCP is "Dalibre mcp": MCP on stdin and stdout, for agents that only
// start stdio servers. It passes everything on to the running app, or
// serves the last library read-only when the app isn't running.
func serveMCP() {
	log.SetOutput(os.Stderr) // stdout carries the protocol
	s := settings.Load(settings.DefaultPath())
	root := os.Getenv("DALIBRE_LIBRARY")
	if root == "" {
		root = s.Library
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := mcp.Bridge(ctx, mcp.Endpoint(s.MCP.ServerPort()), s.MCP.Token, root, mygo.App.Version(), os.Stdin, os.Stdout)
	if err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
