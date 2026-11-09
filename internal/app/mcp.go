package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"syscall"
	"time"

	"dalibre/internal/calibre"
	"dalibre/internal/mcp"
	"dalibre/internal/plugin"
)

// Serving the library to the user's own agents over MCP (docs/mcp/design.md):
// a server on this computer's loopback address, started and stopped by
// Settings, that agents reach with the token kept in the settings.

// mcpState is the MCP server, while it runs or failed to start.
type mcpState struct {
	server *http.Server
	mcp    *mcp.Server
	port   int
	err    error
}

// applyMCP starts, stops or restarts the MCP server to match the
// settings. It makes the token when there is none.
func (a *App) applyMCP() {
	cfg := &a.settings.MCP
	if cfg.On && cfg.Token == "" {
		cfg.Token = mcp.NewToken()
		a.saveSettings()
	}
	port := cfg.ServerPort()
	if cur := a.mcpSrv; cur != nil && cur.server != nil {
		if cfg.On && cur.port == port {
			cur.mcp.SetAllowChanges(cfg.AllowsChanges())
			cur.mcp.SetToken(cfg.Token)
			return
		}
		srv := cur.server
		go srv.Close()
	}
	a.mcpSrv = nil
	if !cfg.On {
		return
	}
	st := &mcpState{port: port}
	a.mcpSrv = st
	s := mcp.New(mcpHost{a}, a.au.version, cfg.AllowsChanges())
	s.SetToken(cfg.Token)
	s.OnUse = func() { a.post(func() {}) } // the status shows who calls what
	a.run(func() {
		ln, err := mcp.Listen(port)
		srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
		a.post(func() {
			if a.mcpSrv != st {
				if ln != nil {
					ln.Close()
				}
				return
			}
			if err != nil {
				st.err = err
				return
			}
			st.server, st.mcp = srv, s
			go srv.Serve(ln)
		})
	})
}

// mcpStatus is how the MCP server is, as Settings shows it.
type mcpStatus struct {
	Running bool
	Port    int
	// Err is why the server didn't start; PortInUse when another program
	// has the port.
	Err       error
	PortInUse bool
	// Client is the name the agent that called a tool last gave in its
	// handshake, as "claude-code"; Used is when, zero when none has.
	Client string
	Used   time.Time
	// Active are the agents calling tools now or lately, the most recent
	// first.
	Active []mcp.Agent
}

func (a *App) mcpStatus() mcpStatus {
	st := a.mcpSrv
	if st == nil {
		return mcpStatus{Port: a.settings.MCP.ServerPort()}
	}
	out := mcpStatus{Running: st.server != nil, Port: st.port, Err: st.err, PortInUse: errors.Is(st.err, syscall.EADDRINUSE)}
	if st.mcp != nil {
		out.Client, out.Used = st.mcp.LastUse()
		now := time.Now()
		for _, ag := range st.mcp.Agents() {
			if ag.Active(now) {
				out.Active = append(out.Active, ag)
			}
		}
	}
	return out
}

// mcpAgents are the agents that called tools since the server started,
// the most recent first.
func (a *App) mcpAgents() []mcp.Agent {
	if st := a.mcpSrv; st != nil && st.mcp != nil {
		return st.mcp.Agents()
	}
	return nil
}

// mcpCalls are the last n tool calls, the newest first; all the server
// keeps when n <= 0.
func (a *App) mcpCalls(n int) []mcp.Call {
	if st := a.mcpSrv; st != nil && st.mcp != nil {
		return st.mcp.Calls(n)
	}
	return nil
}

// clearMCPCalls forgets the tool calls so far.
func (a *App) clearMCPCalls() {
	if st := a.mcpSrv; st != nil && st.mcp != nil {
		st.mcp.ClearCalls()
	}
}

// mcpEndpoint is the address agents connect to.
func (a *App) mcpEndpoint() string { return mcp.Endpoint(a.settings.MCP.ServerPort()) }

// mcpToken is the token agents send.
func (a *App) mcpToken() string { return a.settings.MCP.Token }

// regenerateMCPToken replaces the token; the old one stops working at
// once, and agents set up with it need the new one.
func (a *App) regenerateMCPToken() {
	a.settings.MCP.Token = mcp.NewToken()
	a.saveSettings()
	if st := a.mcpSrv; st != nil && st.mcp != nil {
		st.mcp.SetToken(a.settings.MCP.Token)
	}
}

// mcpSnippet is how one agent is set up to use the server.
type mcpSnippet struct {
	Client string // "Claude Code"
	Text   string
}

// mcpSnippets set up Claude Code, Claude Desktop and Cursor. Claude
// Desktop starts stdio servers only, so it runs this app's stdio bridge,
// which reads the token from the settings.
func (a *App) mcpSnippets() []mcpSnippet {
	url, token := a.mcpEndpoint(), a.mcpToken()
	exe, err := os.Executable()
	if err != nil {
		exe = "Dalibre"
	}
	indent := func(v any) string {
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}
	type obj = map[string]any
	return []mcpSnippet{
		{"Claude Code", `claude mcp add --transport http dalibre ` + url + ` --header "Authorization: Bearer ` + token + `"`},
		{"Claude Desktop", indent(obj{"mcpServers": obj{"dalibre": obj{"command": exe, "args": []string{"mcp"}}}})},
		{"Cursor", indent(obj{"mcpServers": obj{"dalibre": obj{"url": url, "headers": obj{"Authorization": "Bearer " + token}}}})},
	}
}

// mcpHost is the app as the MCP server sees it. Its methods run on the
// server's goroutines.
type mcpHost struct{ a *App }

func (h mcpHost) Library() *calibre.Library { return h.a.libShared.Load() }

func (h mcpHost) Changed(ids ...int64) {
	h.a.post(func() {
		for _, id := range ids {
			h.a.covers.forget(id)
		}
		h.a.reload(nil)
	})
}

func (h mcpHost) Trash() func(path string) error { return h.a.trash }

// onMain runs fn on the main thread and waits for it.
func (h mcpHost) onMain(ctx context.Context, fn func()) bool {
	done := make(chan struct{})
	h.a.post(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

func (h mcpHost) BookSources(ctx context.Context) []*plugin.Plugin {
	var out []*plugin.Plugin
	if h.onMain(ctx, func() { out = h.a.sources() }) {
		return out
	}
	return nil
}

func (h mcpHost) MetadataSources(ctx context.Context) []*plugin.Plugin {
	var out []*plugin.Plugin
	if h.onMain(ctx, func() { out = h.a.metadataSources() }) {
		return out
	}
	return nil
}

// Download adds r to the library as Get books' downloadBook does.
func (h mcpHost) Download(ctx context.Context, p *plugin.Plugin, r plugin.Result, format string) (int64, error) {
	lib := h.Library()
	if lib == nil {
		return 0, errors.New("no library is open in Dalibre")
	}
	dir, err := os.MkdirTemp("", "dalibre-download-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	file, err := p.Download(ctx, r, format, dir, nil)
	if err != nil {
		return 0, err
	}
	cover, _ := p.Cover(ctx, r) // the file's own cover comes first; a missing one is no failure
	id, err := lib.ImportWith(file, resultMetadata(r), cover)
	if err != nil {
		return 0, err
	}
	h.Changed()
	return id, nil
}
