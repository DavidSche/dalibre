package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"dalibre/internal/settings"
)

// bearerTransport sends the MCP token.
type bearerTransport string

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

func TestMCPServer(t *testing.T) {
	a := newTestApp(t)
	port := freePort(t)
	a.settings.MCP = settings.MCP{On: true, Port: port}
	a.applyMCP()
	defer func() {
		a.settings.MCP.On = false
		a.applyMCP()
	}()
	st := a.mcpStatus()
	if !st.Running || st.Port != port || st.Err != nil || !st.Used.IsZero() {
		t.Fatalf("status %+v", st)
	}
	token := a.mcpToken()
	if len(token) != 64 {
		t.Fatalf("token %q", token)
	}
	if want := "http://127.0.0.1:" + strconv.Itoa(port) + "/mcp"; a.mcpEndpoint() != want {
		t.Errorf("endpoint %q", a.mcpEndpoint())
	}

	ctx := context.Background()
	connect := func(token string) (*sdk.ClientSession, error) {
		return sdk.NewClient(&sdk.Implementation{Name: "claude-code"}, nil).Connect(ctx, &sdk.StreamableClientTransport{
			Endpoint: a.mcpEndpoint(), HTTPClient: &http.Client{Transport: bearerTransport(token)}, MaxRetries: -1,
		}, nil)
	}
	var cs *sdk.ClientSession
	var err error
	for range 20 { // Serve starts on its own goroutine
		if cs, err = connect(token); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	// An agent's edit shows in the window.
	dune := int64(0)
	for _, b := range a.books {
		if b.Title == "Dune" {
			dune = b.ID
		}
	}
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "update_books", Arguments: map[string]any{"ids": []int64{dune}, "patch": map[string]any{"add_tags": []string{"Desert"}}}})
	if err != nil || res.IsError {
		t.Fatalf("update: %v %+v", err, res)
	}
	if !slices.Contains(a.byID[dune].Tags, "Desert") {
		t.Errorf("window not reloaded: %+v", a.byID[dune])
	}
	if st := a.mcpStatus(); st.Client != "claude-code" || time.Since(st.Used) > time.Minute {
		t.Errorf("last use %+v", st)
	}

	// Read only: the write tools go.
	a.settings.MCP.ReadOnly = true
	a.applyMCP()
	res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "update_books", Arguments: map[string]any{"ids": []int64{dune}, "patch": map[string]any{}}})
	if err == nil && !res.IsError {
		t.Error("update allowed while read-only")
	}

	// A new token: the old one is refused at once.
	a.regenerateMCPToken()
	if a.mcpToken() == token {
		t.Fatal("same token")
	}
	if _, err := connect(token); err == nil {
		t.Error("old token let in")
	}
	cs2, err := connect(a.mcpToken())
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	cs2.Close()

	snippets := a.mcpSnippets()
	if len(snippets) != 3 || !strings.Contains(snippets[0].Text, "--transport http dalibre "+a.mcpEndpoint()+` --header "Authorization: Bearer `+a.mcpToken()+`"`) {
		t.Fatalf("snippets %+v", snippets)
	}
	var desktop, cursor struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	json.Unmarshal([]byte(snippets[1].Text), &desktop)
	json.Unmarshal([]byte(snippets[2].Text), &cursor)
	if d := desktop.MCPServers["dalibre"]; d.Command == "" || !slices.Equal(d.Args, []string{"mcp"}) || strings.Contains(snippets[1].Text, a.mcpToken()) {
		t.Errorf("Claude Desktop %s", snippets[1].Text)
	}
	if c := cursor.MCPServers["dalibre"]; c.URL != a.mcpEndpoint() || c.Headers["Authorization"] != "Bearer "+a.mcpToken() {
		t.Errorf("Cursor %s", snippets[2].Text)
	}
}

func TestMCPPortInUse(t *testing.T) {
	a := newTestApp(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a.settings.MCP = settings.MCP{On: true, Port: ln.Addr().(*net.TCPAddr).Port}
	a.applyMCP()
	if st := a.mcpStatus(); st.Running || !st.PortInUse {
		t.Fatalf("status %+v", st)
	}
	a.settings.MCP.On = false
	a.applyMCP()
	if a.mcpSrv != nil {
		t.Error("still there")
	}
}
