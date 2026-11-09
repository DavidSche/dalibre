package mcp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"dalibre/internal/calibre"
)

// readOnly is the Host of a library opened by the stdio bridge while
// the app isn't running.
type readOnly struct{ lib *calibre.Library }

func (h readOnly) Library() *calibre.Library    { return h.lib }
func (readOnly) Changed(...int64)               {}
func (readOnly) Trash() func(path string) error { return nil }

// bearer adds the token to each request.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// Bridge serves MCP on in and out, for clients that only start stdio
// servers. While the app serves endpoint it passes everything on to the
// app with token; otherwise it serves the read tools of the library in
// root, opened read-only.
func Bridge(ctx context.Context, endpoint, token, root, version string, in io.ReadCloser, out io.WriteCloser) error {
	t := &sdk.IOTransport{Reader: in, Writer: out}
	if token != "" && answers(endpoint) {
		if s, close, err := proxy(ctx, endpoint, token, version); err == nil {
			defer close()
			return s.Run(ctx, t)
		}
	}
	var h readOnly
	if root != "" {
		if lib, err := calibre.OpenReadOnly(root); err == nil {
			defer lib.Close()
			h.lib = lib
		}
	}
	return New(h, version, false).MCP().Run(ctx, t)
}

// answers reports whether something listens at endpoint.
func answers(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	c, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// proxy connects to the app's server and makes a server with its tools
// that calls them there, naming the agent it serves. close ends the
// connection.
func proxy(ctx context.Context, endpoint, token, version string) (srv *sdk.Server, close func(), err error) {
	var (
		mu    sync.Mutex
		names []string
		cs    *sdk.ClientSession
	)
	call := func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		p := &sdk.CallToolParams{Name: req.Params.Name, Arguments: req.Params.Arguments}
		if req.Session != nil {
			if ip := req.Session.InitializeParams(); ip != nil && ip.ClientInfo != nil {
				p.Meta = sdk.Meta{clientMeta: forward(ip.ClientInfo)}
			}
		}
		return cs.CallTool(ctx, p)
	}
	refresh := func(ctx context.Context) {
		var tools []*sdk.Tool
		for t, err := range cs.Tools(ctx, nil) {
			if err != nil {
				return
			}
			tools = append(tools, t)
		}
		mu.Lock()
		defer mu.Unlock()
		if srv == nil {
			return
		}
		srv.RemoveTools(names...)
		names = names[:0]
		for _, t := range tools {
			srv.AddTool(t, call)
			names = append(names, t.Name)
		}
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "dalibre-bridge", Version: version}, &sdk.ClientOptions{
		ToolListChangedHandler: func(ctx context.Context, _ *sdk.ToolListChangedRequest) {
			go refresh(context.WithoutCancel(ctx))
		},
	})
	cs, err = client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Transport: bearer{token, http.DefaultTransport}},
	}, nil)
	if err != nil {
		return nil, nil, err
	}
	opts := &sdk.ServerOptions{Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{ListChanged: true}}}
	if r := cs.InitializeResult(); r != nil {
		opts.Instructions = r.Instructions
	}
	mu.Lock()
	srv = sdk.NewServer(&sdk.Implementation{Name: "dalibre", Title: "Dalibre", Version: version}, opts)
	mu.Unlock()
	refresh(ctx)
	return srv, func() { cs.Close() }, nil
}

// forward is the agent c as the bridge names it to the app: its name,
// title and the one icon the app would keep.
func forward(c *sdk.Implementation) clientInfo {
	ci := clientInfo{Name: c.Name, Title: c.Title}
	if _, _, i, ok := pickIcon(c.Icons); ok {
		ci.Icons = c.Icons[i : i+1]
	}
	return ci
}
