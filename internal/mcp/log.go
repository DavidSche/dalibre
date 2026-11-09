package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The call log: the agents that called tools and their recent calls, kept
// in memory for the window.

// ActiveWindow is how long after its last call an agent counts as active.
const ActiveWindow = 30 * time.Second

const (
	maxCalls    = 500     // calls the log keeps
	maxArgs     = 4 << 10 // bytes of a call's arguments it keeps
	maxBookRefs = 20      // books a call names
	maxIcon     = 256 << 10
)

// Agent is a client that called tools, by the name it gives in its
// handshake.
type Agent struct {
	Name  string // as "claude-code"
	Title string // as "Claude Code"; empty when it gave none
	// Icon is the image of its first data: icon that is SVG, PNG or JPEG,
	// and IconType that image's MIME type; nil when it has none.
	Icon     []byte
	IconType string
	Calls    int       // tool calls it made
	Last     time.Time // when its last call started or finished
	InFlight int       // calls running now
}

// Active reports whether a calls a tool now or did within ActiveWindow.
func (a Agent) Active(now time.Time) bool {
	return a.InFlight > 0 || (!a.Last.IsZero() && now.Sub(a.Last) < ActiveWindow)
}

// BookRef is a book a call read or changed.
type BookRef struct {
	ID    int64
	Title string
}

// Call is one tool call.
type Call struct {
	ID    int64 // increasing
	Agent string
	Tool  string
	// Args are the arguments as received, cut to 4 KB; ArgsTruncated when
	// they were cut, and so aren't valid JSON.
	Args          json.RawMessage
	ArgsTruncated bool
	Start         time.Time
	Duration      time.Duration // zero while it runs
	Done          bool
	Err           string // why it failed; empty when it didn't
	Write         bool   // the tool changes the library
	Count         int    // books returned or affected; -1 when that doesn't apply
	Books         []BookRef
}

// clientInfo is what the stdio bridge passes on under clientMeta: the
// agent it serves.
type clientInfo struct {
	Name  string     `json:"name"`
	Title string     `json:"title,omitempty"`
	Icons []sdk.Icon `json:"icons,omitempty"`
}

// caller is the agent behind req: the one the bridge names, or the
// session's client.
func caller(req *sdk.CallToolRequest) clientInfo {
	if v, ok := req.Params.Meta[clientMeta]; ok {
		var ci clientInfo
		if b, err := json.Marshal(v); err == nil && json.Unmarshal(b, &ci) == nil && ci.Name != "" {
			return ci
		}
	}
	if req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil {
			return clientInfo{Name: p.ClientInfo.Name, Title: p.ClientInfo.Title, Icons: p.ClientInfo.Icons}
		}
	}
	return clientInfo{}
}

// iconTypes are the icon images the log keeps.
var iconTypes = []string{"image/svg+xml", "image/png", "image/jpeg"}

// pickIcon is the first of icons that is a data: URI of an image the log
// keeps, decoded; ok is false when there is none. URLs aren't fetched.
func pickIcon(icons []sdk.Icon) (img []byte, mime string, i int, ok bool) {
	for i, ic := range icons {
		if img, mime, ok := decodeIcon(ic); ok {
			return img, mime, i, true
		}
	}
	return nil, "", -1, false
}

func decodeIcon(ic sdk.Icon) ([]byte, string, bool) {
	rest, ok := strings.CutPrefix(ic.Source, "data:")
	if !ok || len(rest) > 3*maxIcon {
		return nil, "", false
	}
	head, data, ok := strings.Cut(rest, ",")
	if !ok {
		return nil, "", false
	}
	params := strings.Split(head, ";")
	mime := strings.ToLower(strings.TrimSpace(params[0]))
	if mime == "" {
		mime = strings.ToLower(ic.MIMEType)
	}
	if !slices.Contains(iconTypes, mime) {
		return nil, "", false
	}
	var img []byte
	var err error
	if slices.Contains(params[1:], "base64") {
		img, err = base64.StdEncoding.DecodeString(data)
		if err != nil {
			img, err = base64.RawStdEncoding.DecodeString(data)
		}
	} else {
		var s string
		s, err = url.PathUnescape(data)
		img = []byte(s)
	}
	if err != nil || len(img) == 0 || len(img) > maxIcon {
		return nil, "", false
	}
	return img, mime, true
}

// isWrite reports whether tool changes the library.
func isWrite(tool string) bool {
	return slices.Contains(writeTools, tool) || tool == "download_book"
}

// begin logs the start of a call by ci and returns its record.
func (s *Server) begin(ci clientInfo, tool string, args json.RawMessage) *Call {
	now := time.Now()
	c := &Call{Agent: ci.Name, Tool: tool, Start: now, Write: isWrite(tool), Count: -1}
	if len(args) > maxArgs {
		c.Args, c.ArgsTruncated = slices.Clone(args[:maxArgs]), true
	} else {
		c.Args = slices.Clone(args)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client, s.used = ci.Name, now
	s.nextCall++
	c.ID = s.nextCall
	s.calls = append(s.calls, c)
	if len(s.calls) > maxCalls {
		s.calls = slices.Delete(s.calls, 0, len(s.calls)-maxCalls)
	}
	a := s.agents[ci.Name]
	if a == nil {
		a = &Agent{Name: ci.Name}
		s.agents[ci.Name] = a
	}
	if ci.Title != "" {
		a.Title = ci.Title
	}
	if len(ci.Icons) > 0 && a.Icon == nil {
		if img, mime, _, ok := pickIcon(ci.Icons); ok {
			a.Icon, a.IconType = img, mime
		}
	}
	a.Calls++
	a.InFlight++
	a.Last = now
	return c
}

// end logs the end of call c with its result.
func (s *Server) end(c *Call, res sdk.Result, err error) {
	now := time.Now()
	msg := ""
	if err != nil {
		msg = err.Error()
	} else if r, ok := res.(*sdk.CallToolResult); ok && r.IsError {
		msg = "failed"
		for _, ct := range r.Content {
			if t, ok := ct.(*sdk.TextContent); ok {
				msg = t.Text
				break
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c.Duration, c.Done, c.Err = max(now.Sub(c.Start), time.Nanosecond), true, msg
	if a := s.agents[c.Agent]; a != nil {
		a.InFlight--
		a.Last = now
	}
}

type recorderKey struct{}

// noteBooks records, for the call ctx carries, how many books it returned
// or affected and which; it does nothing outside a logged call.
func (s *Server) noteBooks(ctx context.Context, n int, books ...BookRef) {
	c, _ := ctx.Value(recorderKey{}).(*Call)
	if c == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c.Count = n
	c.Books = slices.Clone(books[:min(len(books), maxBookRefs)])
}

// Agents are the agents that called tools, the most recent first.
func (s *Server) Agents() []Agent {
	s.mu.Lock()
	out := make([]Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, *a)
	}
	s.mu.Unlock()
	slices.SortFunc(out, func(a, b Agent) int {
		if c := b.Last.Compare(a.Last); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// Calls are the last n tool calls, the newest first; all the log keeps
// when n <= 0.
func (s *Server) Calls(n int) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.calls) {
		n = len(s.calls)
	}
	out := make([]Call, 0, n)
	for i := len(s.calls) - 1; i >= 0 && len(out) < n; i-- {
		c := *s.calls[i]
		c.Books = slices.Clone(c.Books)
		out = append(out, c)
	}
	return out
}

// ClearCalls forgets the calls logged so far; the agents stay.
func (s *Server) ClearCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}
