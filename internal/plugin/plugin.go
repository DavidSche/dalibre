package plugin

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"modernc.org/quickjs"
)

// Limits on one call into a plugin and on its interpreter.
const (
	// CallTimeout bounds one call when the caller's context has no
	// earlier deadline.
	CallTimeout = 60 * time.Second
	// MaxMemory is how much memory a plugin's interpreter may use.
	MaxMemory = 64 << 20
)

//go:embed prelude.js
var prelude string

// Plugin is a loaded plugin. Its methods may be called from any
// goroutine; calls into its script run one at a time.
type Plugin struct {
	Manifest *Manifest
	// Dir is the plugin's folder; "" for one built into Dalibre.
	Dir string
	// Builtin is set for a plugin that ships inside Dalibre.
	Builtin bool

	fsys fs.FS // the plugin's files
	// native is the Go source behind a plugin made by NewNative.
	native Native
	// authorize adds credentials to the plugin's requests.
	authorize func(*http.Request)

	settings map[string]string
	client   *http.Client
	logf     func(format string, args ...any)

	mu       sync.Mutex // guards the fields below and the interpreter
	vm       *quickjs.VM
	exported []string        // the functions the script exports
	ctx      context.Context // of the call in progress, for fetch
	requests int             // fetches made by the call in progress
	settled  *string         // the outcome of the call in progress, as JSON
	srcSize  int64           // bytes of script loaded into vm
	closed   bool
}

// Options configure a loaded plugin.
type Options struct {
	// Settings are the user's values for the manifest's settings; missing
	// ones take their defaults.
	Settings map[string]string
	// Logf receives the plugin's log lines; log.Printf when nil.
	Logf func(format string, args ...any)
	// Transport makes the plugin's requests, for tests. When nil requests
	// go through the sandbox's transport, which refuses local addresses
	// unless the manifest asks for the local network.
	Transport http.RoundTripper
	// Authorize, when set, is called on every request the plugin makes,
	// to add credentials: the user's account on an OPDS server.
	Authorize func(*http.Request)
}

// Load reads the plugin in dir and runs its script, which defines the
// functions the host calls. Close it when done.
func Load(dir string, opts Options) (*Plugin, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	return load(m, os.DirFS(dir), dir, opts)
}

// LoadBuiltin loads a plugin that ships inside Dalibre, from fsys holding
// its plugin.json and scripts. It may use a reserved id.
func LoadBuiltin(fsys fs.FS, opts Options) (*Plugin, error) {
	data, err := fs.ReadFile(fsys, "plugin.json")
	if err != nil {
		return nil, err
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	p, err := load(m, fsys, "", opts)
	if p != nil {
		p.Builtin = true
	}
	return p, err
}

func load(m *Manifest, fsys fs.FS, dir string, opts Options) (*Plugin, error) {
	p := &Plugin{Manifest: m, Dir: dir, fsys: fsys, ctx: context.Background(), logf: opts.Logf, settings: map[string]string{}}
	if p.logf == nil {
		p.logf = log.Printf
	}
	for _, s := range m.Settings {
		v, ok := opts.Settings[s.Key]
		if !ok {
			v = s.Default
		}
		p.settings[s.Key] = v
	}
	p.client = p.httpClient(opts.Transport)
	p.authorize = opts.Authorize

	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.start(); err != nil {
		return nil, err
	}
	if err := p.checkExports(); err != nil {
		p.stop()
		return nil, err
	}
	return p, nil
}

// checkExports checks the script exports what its capabilities need.
func (p *Plugin) checkExports() error {
	m := p.Manifest
	need := []string{}
	if m.IsBookSource() {
		need = append(need, "search")
		if m.HasCollections() {
			need = append(need, "collections", "list")
		}
	}
	for _, n := range need {
		if !slices.Contains(p.exported, n) {
			msg := "a book source must export " + n
			if n != "search" {
				msg = "a book source with collections must export " + n
			}
			return &Error{Plugin: m.ID, Kind: KindInvalid, Msg: msg}
		}
	}
	if m.IsMetadataSource() && !slices.Contains(p.exported, "identify") {
		return &Error{Plugin: m.ID, Kind: KindInvalid, Msg: "a metadata source must export identify"}
	}
	if m.IsReader() && !slices.Contains(p.exported, "open") {
		return &Error{Plugin: m.ID, Kind: KindInvalid, Msg: "a reader must export open"}
	}
	return nil
}

// Close frees the plugin's interpreter. Calls after Close fail.
func (p *Plugin) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.stop()
}

// start makes the plugin's interpreter and loads its script.
func (p *Plugin) start() error {
	vm, err := quickjs.NewVM()
	if err != nil {
		return err
	}
	p.vm, p.srcSize = vm, 0
	vm.SetMemoryLimit(MaxMemory)
	vm.SetEvalTimeout(CallTimeout + 10*time.Second) // a backstop; calls have their own deadline
	vm.SetModuleLoader(p.loadModule, p.resolveModule)
	host := map[string]quickjs.HostFunc{
		"__host_info":   p.hostInfo,
		"__host_fetch":  p.hostFetch,
		"__host_log":    p.hostLog,
		"__host_settle": p.hostSettle,
	}
	for name, fn := range host {
		if err := vm.RegisterHostFunc(name, fn); err != nil {
			p.stop()
			return err
		}
	}
	if _, err := vm.Eval(prelude, quickjs.EvalGlobal); err != nil {
		p.stop()
		return fmt.Errorf("plugin prelude: %w", err)
	}
	var names []string
	if err := p.invoke(context.Background(), "", &names, "__dalibre_load", p.Manifest.Main); err != nil {
		p.stop()
		return err
	}
	p.exported = names
	return nil
}

// stop frees the interpreter; the next call starts a new one, so the
// script's own state starts over.
func (p *Plugin) stop() {
	if p.vm != nil {
		p.vm.Close()
		p.vm = nil
	}
}

// resolveModule turns an import in module base into a path relative to
// the plugin's folder. Only the plugin's own .js files can be imported.
func (p *Plugin) resolveModule(_ *quickjs.VM, base, name string) (string, error) {
	if !strings.HasPrefix(name, "./") && !strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("can’t import %q: only the plugin’s own files can be imported, as \"./lib.js\"; bundle packages into main.js (with esbuild)", name)
	}
	dir := "."
	if base != "<eval>" && base != "" {
		dir = path.Dir(base)
	}
	resolved := path.Clean(path.Join(dir, name))
	if resolved == ".." || strings.HasPrefix(resolved, "../") || path.IsAbs(resolved) {
		return "", fmt.Errorf("can’t import %q: it is outside the plugin’s folder", name)
	}
	if ext := path.Ext(resolved); ext != ".js" && ext != ".mjs" {
		return "", fmt.Errorf("can’t import %q: only .js files can be imported", name)
	}
	return resolved, nil
}

func (p *Plugin) loadModule(_ *quickjs.VM, name string) (string, error) {
	st, err := fs.Stat(p.fsys, name)
	if err != nil || st.IsDir() {
		return "", fmt.Errorf("%s: no such file", name)
	}
	if p.srcSize += st.Size(); p.srcSize > MaxScriptSize {
		return "", fmt.Errorf("the plugin’s scripts are over %d MB", MaxScriptSize>>20)
	}
	data, err := fs.ReadFile(p.fsys, name)
	return string(data), err
}

// Discover loads every plugin in the folders inside root that load
// accepts (all when nil). A folder that fails to load gives an error and
// is skipped.
func Discover(root string, load func(m *Manifest) (Options, bool)) ([]*Plugin, []error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, []error{err}
	}
	var plugins []*Plugin
	var errs []error
	seen := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		m, err := ReadManifest(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue // not a plugin
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if Reserved(m.ID) {
			errs = append(errs, fmt.Errorf("%s: id %q is kept for plugins that ship with Dalibre", e.Name(), m.ID))
			continue
		}
		if other, ok := seen[m.ID]; ok {
			errs = append(errs, fmt.Errorf("%s: id %q is already used by %s", e.Name(), m.ID, other))
			continue
		}
		seen[m.ID] = e.Name()
		o, ok := Options{}, true
		if load != nil {
			o, ok = load(m)
		}
		if !ok {
			continue
		}
		p, err := Load(dir, o)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		plugins = append(plugins, p)
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Manifest.Name < plugins[j].Manifest.Name })
	return plugins, errs
}

// DefaultDir is the plugins folder in the user's configuration folder,
// or $DALIBRE_PLUGINS.
func DefaultDir() string {
	if d := os.Getenv("DALIBRE_PLUGINS"); d != "" {
		return d
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "Dalibre", "plugins")
}

// Defines reports whether the plugin exports a function called name.
func (p *Plugin) Defines(name string) bool {
	if p.native != nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Contains(p.exported, name)
}

// MissingSettings are the required settings the user hasn't given.
func (p *Plugin) MissingSettings() []string {
	var out []string
	for _, s := range p.Manifest.Settings {
		if s.Required && strings.TrimSpace(p.settings[s.Key]) == "" {
			out = append(out, s.Title)
		}
	}
	return out
}

// call runs the exported function name with args and returns its result
// as JSON. Arguments and results cross as JSON so neither side sees the
// other's types. An async function's promise is awaited.
func (p *Plugin) call(ctx context.Context, name string, args ...any) (json.RawMessage, error) {
	if missing := p.MissingSettings(); len(missing) > 0 {
		return nil, &Error{Plugin: p.Manifest.ID, Call: name, Kind: KindSettings,
			Msg: "needs " + strings.Join(missing, ", ") + " in its settings"}
	}
	if args == nil {
		args = []any{}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, &Error{Plugin: p.Manifest.ID, Call: name, Kind: KindUnavailable, Msg: "the plugin was closed"}
	}
	if p.vm == nil {
		// The last call broke the interpreter (out of memory, out of
		// time); start afresh.
		if err := p.start(); err != nil {
			return nil, err
		}
	}
	if !slices.Contains(p.exported, name) {
		return nil, &Error{Plugin: p.Manifest.ID, Call: name, Kind: KindInvalid, Msg: "isn’t exported"}
	}
	var out json.RawMessage
	if err := p.invoke(ctx, name, &out, "__dalibre_call", name, string(data)); err != nil {
		return nil, err
	}
	return out, nil
}

// outcome is how the prelude reports a call.
type outcome struct {
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value"`
	Error struct {
		Name, Message, Stack, Kind string
	} `json:"error"`
}

// invoke calls fn, a function of the prelude, under ctx's deadline or
// CallTimeout, runs the promise jobs it queues, and decodes the value it
// settles with into out. An interpreter that ran out of time or memory is
// stopped, to start afresh on the next call.
func (p *Plugin) invoke(ctx context.Context, call string, out any, fn string, args ...any) (err error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	vm := p.vm
	p.ctx, p.requests, p.settled = ctx, 0, nil
	stopTimer := context.AfterFunc(ctx, vm.Interrupt)
	defer func() {
		stopTimer()
		p.ctx = context.Background()
		if r := recover(); r != nil {
			// The engine gave up on this interpreter; only closing is left.
			p.stop()
			err = &Error{Plugin: p.Manifest.ID, Call: call, Kind: KindScript, Msg: fmt.Sprint("the interpreter failed: ", r)}
		}
	}()

	_, jsErr := vm.Call(fn, args...)
	if jsErr == nil {
		_, jsErr = vm.ExecutePendingJobs()
	}
	fail := func(kind ErrorKind, msg string) error {
		p.stop()
		return &Error{Plugin: p.Manifest.ID, Call: call, Kind: kind, Msg: msg, Err: ctx.Err()}
	}
	switch {
	case ctx.Err() != nil:
		if errors.Is(ctx.Err(), context.Canceled) {
			return fail(KindTimeout, "canceled")
		}
		return fail(KindTimeout, "took too long")
	case outOfMemory(jsErr) || p.settled != nil && strings.Contains(*p.settled, `"message":"out of memory"`):
		return fail(KindScript, fmt.Sprintf("used more than %d MB of memory", MaxMemory>>20))
	case p.settled == nil && jsErr != nil:
		return fail(KindScript, jsErr.Error())
	case p.settled == nil:
		return &Error{Plugin: p.Manifest.ID, Call: call, Kind: KindInvalid,
			Msg: "returned a promise that never settled; await only dalibre.fetch and your own async functions"}
	}

	var o outcome
	if err := json.Unmarshal([]byte(*p.settled), &o); err != nil {
		return &Error{Plugin: p.Manifest.ID, Call: call, Kind: KindInvalid, Msg: "returned what isn’t JSON"}
	}
	if !o.OK {
		return p.thrown(call, o)
	}
	if out != nil {
		if err := json.Unmarshal(o.Value, out); err != nil {
			return &Error{Plugin: p.Manifest.ID, Call: call, Kind: KindInvalid, Msg: "returned an unexpected shape: " + jsonProblem(err)}
		}
	}
	return nil
}

func outOfMemory(err error) bool {
	var qe *quickjs.Error
	return errors.As(err, &qe) && qe.Message == "out of memory"
}

// thrown makes an Error of what the script threw: an error made by
// dalibre.error, a host error passed through (a refused fetch), or any
// other value.
func (p *Plugin) thrown(call string, o outcome) *Error {
	// Frames in the prelude (file "<eval>") mean nothing to the author.
	var stack []string
	for line := range strings.Lines(o.Error.Stack) {
		if !strings.Contains(line, "<eval>") {
			stack = append(stack, line)
		}
	}
	e := &Error{Plugin: p.Manifest.ID, Call: call, Kind: KindScript, Msg: o.Error.Message, Stack: strings.TrimSpace(strings.Join(stack, ""))}
	if e.Msg == "" {
		e.Msg = "threw an error with no message"
	}
	if k := ErrorKind(o.Error.Kind); k != "" {
		if slices.Contains(thrownKinds, k) || k == KindPermission || k == KindTimeout {
			e.Kind = k
		} else {
			e.Msg = fmt.Sprintf("%s (unknown error kind %q)", e.Msg, k)
		}
	}
	if e.Kind == KindScript && o.Error.Name != "" && o.Error.Name != "Error" {
		e.Msg = o.Error.Name + ": " + e.Msg // TypeError: …
	}
	return e
}
