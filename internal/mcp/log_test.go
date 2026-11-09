package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var png = []byte("\x89PNG\r\n\x1a\nfake")

func pngURI(b []byte) string { return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b) }

// connectAs runs s and returns a client of it that gives impl in its
// handshake.
func connectAs(t *testing.T, s *Server, impl *sdk.Implementation) *sdk.ClientSession {
	t.Helper()
	st, ct := sdk.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := s.MCP().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(impl, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func agent(s *Server, name string) (Agent, bool) {
	for _, a := range s.Agents() {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

func TestDecodeIcon(t *testing.T) {
	big := make([]byte, maxIcon+1)
	for _, c := range []struct {
		name string
		icon sdk.Icon
		want string // the MIME type; empty when the icon is ignored
		img  []byte
	}{
		{"png", sdk.Icon{Source: pngURI(png)}, "image/png", png},
		{"svg, percent-encoded", sdk.Icon{Source: "data:image/svg+xml,%3Csvg%2F%3E"}, "image/svg+xml", []byte("<svg/>")},
		{"type from the icon", sdk.Icon{Source: "data:;base64," + base64.StdEncoding.EncodeToString(png), MIMEType: "image/jpeg"}, "image/jpeg", png},
		{"url", sdk.Icon{Source: "https://example.com/icon.png", MIMEType: "image/png"}, "", nil},
		{"gif", sdk.Icon{Source: "data:image/gif;base64,R0lGOD=="}, "", nil},
		{"bad base64", sdk.Icon{Source: "data:image/png;base64,!!!"}, "", nil},
		{"oversize", sdk.Icon{Source: pngURI(big)}, "", nil},
	} {
		img, mime, ok := decodeIcon(c.icon)
		if ok != (c.want != "") || mime != c.want || !bytes.Equal(img, c.img) {
			t.Errorf("%s: %q %q %v", c.name, img, mime, ok)
		}
	}
}

func TestAgents(t *testing.T) {
	lib := testLibrary(t)
	s := New(&testHost{lib: lib}, "test", false)
	var uses atomic.Int32
	s.OnUse = func() { uses.Add(1) }

	cs := connectAs(t, s, &sdk.Implementation{Name: "claude-code", Title: "Claude Code", Version: "1", Icons: []sdk.Icon{
		{Source: "https://example.com/icon.png", MIMEType: "image/png"},
		{Source: pngURI(make([]byte, maxIcon+1))},
		{Source: pngURI(png)},
	}})
	other := connectAs(t, s, &sdk.Implementation{Name: "cursor", Version: "1", Icons: []sdk.Icon{{Source: "https://example.com/cursor.svg"}}})

	call(t, cs, "search_books", map[string]any{}, nil)
	call(t, cs, "list_facets", map[string]any{"kind": "tags"}, nil)
	time.Sleep(time.Millisecond)
	call(t, other, "search_books", map[string]any{"query": "emma"}, nil)
	if n := uses.Load(); n != 6 {
		t.Errorf("OnUse told %d times, want a start and an end for each of 3 calls", n)
	}

	as := s.Agents()
	if len(as) != 2 || as[0].Name != "cursor" || as[1].Name != "claude-code" {
		t.Fatalf("agents: %+v", as)
	}
	cc := as[1]
	if cc.Title != "Claude Code" || cc.IconType != "image/png" || !bytes.Equal(cc.Icon, png) || cc.Calls != 2 || cc.InFlight != 0 {
		t.Errorf("claude-code: %+v", cc)
	}
	if c := as[0]; c.Title != "" || c.Icon != nil || c.Calls != 1 {
		t.Errorf("cursor: %+v", c)
	}
	now := time.Now()
	if !cc.Active(now) || cc.Active(cc.Last.Add(ActiveWindow)) {
		t.Errorf("active window: %v", cc.Last)
	}
	if name, when := s.LastUse(); name != "cursor" || when.IsZero() {
		t.Errorf("last use: %q %v", name, when)
	}
}

func TestInFlight(t *testing.T) {
	s := New(&testHost{lib: testLibrary(t)}, "test", false)
	started, release := make(chan struct{}), make(chan struct{})
	sdk.AddTool(s.MCP(), readTool("slow", "Waits."), func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, struct{}, error) {
		close(started)
		<-release
		return nil, struct{}{}, nil
	})
	cs := connectAs(t, s, &sdk.Implementation{Name: "agent", Version: "1"})
	done := make(chan string)
	go func() {
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "slow", Arguments: map[string]any{}})
		done <- fmt.Sprint(err, res != nil && res.IsError)
	}()
	<-started
	a, _ := agent(s, "agent")
	if a.InFlight != 1 || !a.Active(time.Now().Add(time.Hour)) {
		t.Errorf("while running: %+v", a)
	}
	if c := s.Calls(1); len(c) != 1 || c[0].Done || c[0].Duration != 0 || c[0].Tool != "slow" {
		t.Errorf("running call: %+v", c)
	}
	close(release)
	if got := <-done; got != "<nil> false" {
		t.Fatal(got)
	}
	a, _ = agent(s, "agent")
	if a.InFlight != 0 || a.Active(time.Now().Add(time.Hour)) {
		t.Errorf("after: %+v", a)
	}
	if c := s.Calls(1)[0]; !c.Done || c.Duration <= 0 || c.Err != "" {
		t.Errorf("finished call: %+v", c)
	}
}

func TestCallLog(t *testing.T) {
	lib := testLibrary(t)
	s := New(&testHost{lib: lib}, "test", true)
	cs := connect(t, s)

	var sr searchOut
	call(t, cs, "search_books", map[string]any{"author": "Ursula K. Le Guin"}, &sr)
	c := s.Calls(1)[0]
	if c.ID != 1 || c.Agent != "test-agent" || c.Tool != "search_books" || c.Write || c.Count != 2 || c.Books != nil ||
		string(c.Args) != `{"author":"Ursula K. Le Guin"}` || c.ArgsTruncated || !c.Done || c.Err != "" {
		t.Errorf("search: %+v", c)
	}
	call(t, cs, "list_facets", map[string]any{"kind": "tags"}, nil)
	if c := s.Calls(1)[0]; c.Count != -1 {
		t.Errorf("facets count: %d", c.Count)
	}
	call(t, cs, "get_book", map[string]any{"id": sr.Books[0].ID}, nil)
	if c := s.Calls(1)[0]; c.Count != 1 || len(c.Books) != 1 || c.Books[0] != (BookRef{sr.Books[0].ID, sr.Books[0].Title}) {
		t.Errorf("get_book: %+v", c)
	}

	ids := []int64{sr.Books[0].ID, sr.Books[1].ID}
	call(t, cs, "update_books", map[string]any{"ids": ids, "patch": map[string]any{"add_tags": []string{"Classics"}}}, nil)
	c = s.Calls(1)[0]
	if !c.Write || c.Count != 2 || len(c.Books) != 2 || c.Books[0].ID != ids[0] || c.Books[0].Title != sr.Books[0].Title {
		t.Errorf("update: %+v", c)
	}
	call(t, cs, "remove_books", map[string]any{"ids": ids[1:]}, nil)
	c = s.Calls(1)[0]
	if !c.Write || c.Count != 1 || len(c.Books) != 1 || c.Books[0] != (BookRef{ids[1], sr.Books[1].Title}) {
		t.Errorf("remove keeps the title: %+v", c)
	}

	// A failing call.
	call(t, cs, "get_book", map[string]any{"id": 999}, nil)
	c = s.Calls(1)[0]
	if !c.Done || c.Err != "no book 999" || c.Count != -1 {
		t.Errorf("failure: %+v", c)
	}
	call(t, cs, "list_facets", map[string]any{"kind": "colours"}, nil)
	if c := s.Calls(1)[0]; !strings.Contains(c.Err, "unknown kind") {
		t.Errorf("failure: %+v", c)
	}

	// Long arguments are cut.
	call(t, cs, "search_books", map[string]any{"query": strings.Repeat("x", 5000)}, nil)
	if c := s.Calls(1)[0]; !c.ArgsTruncated || len(c.Args) != maxArgs {
		t.Errorf("long args: %d %v", len(c.Args), c.ArgsTruncated)
	}

	all := s.Calls(0)
	if len(all) != 8 || all[0].ID != 8 || all[7].ID != 1 {
		t.Errorf("calls: %d, %d..%d", len(all), all[0].ID, all[len(all)-1].ID)
	}

	// The log keeps the last 500.
	for range maxCalls {
		call(t, cs, "list_facets", map[string]any{"kind": "tags"}, nil)
	}
	all = s.Calls(0)
	if len(all) != maxCalls || all[0].ID != 8+maxCalls || all[maxCalls-1].ID != 9 {
		t.Errorf("cap: %d, %d..%d", len(all), all[0].ID, all[len(all)-1].ID)
	}
	if got := len(s.Calls(3)); got != 3 {
		t.Errorf("Calls(3): %d", got)
	}

	s.ClearCalls()
	if n := len(s.Calls(0)); n != 0 {
		t.Errorf("after clear: %d", n)
	}
	if a, ok := agent(s, "test-agent"); !ok || a.Calls != 8+maxCalls {
		t.Errorf("agent after clear: %+v", a)
	}
	call(t, cs, "list_facets", map[string]any{"kind": "tags"}, nil)
	if c := s.Calls(0); len(c) != 1 || c[0].ID != 9+maxCalls {
		t.Errorf("after clear: %+v", c)
	}
}

func TestBridgeForwardsAgent(t *testing.T) {
	lib := testLibrary(t)
	s := New(&testHost{lib: lib}, "test", false)
	s.SetToken("secret")
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Bridge(ctx, srv.URL+Path, "secret", "", "test", inR, outW)
		close(done)
	}()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "claude-ai", Title: "Claude", Version: "1", Icons: []sdk.Icon{
		{Source: "https://example.com/claude.png"}, {Source: pngURI(png)},
	}}, nil).Connect(ctx, &sdk.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cs.Close()
		cancel()
		inW.Close()
		<-done
	}()

	call(t, cs, "search_books", map[string]any{"query": "emma"}, nil)
	as := s.Agents()
	if len(as) != 1 || as[0].Name != "claude-ai" || as[0].Title != "Claude" || as[0].IconType != "image/png" || !bytes.Equal(as[0].Icon, png) {
		t.Fatalf("agents through the bridge: %+v", as)
	}
	if c := s.Calls(1)[0]; c.Agent != "claude-ai" || c.Count != 1 {
		t.Errorf("call through the bridge: %+v", c)
	}
}
