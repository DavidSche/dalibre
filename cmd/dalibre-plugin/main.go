// Command dalibre-plugin is for writing Dalibre plugins: it makes a new
// one, runs it as Dalibre would without the app, packs it, and installs
// packages into the plugins folder.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"dalibre/internal/plugin"
	"dalibre/internal/settings"
	"dalibre/sdk"
)

const usage = `dalibre-plugin is for writing Dalibre plugins.

  dalibre-plugin init <dir> [--id ID] [--name NAME]   start a plugin from the template
  dalibre-plugin check <dir>                          validate plugin.json and load main.js
  dalibre-plugin search <dir> <text> [--filter key=value] [--page N] [--cursor C] [--json]
  dalibre-plugin collections <dir>                    list what the source offers to browse
  dalibre-plugin list <dir> <collection> [--filter key=value] [--page N] [--json]
  dalibre-plugin get <dir> <text> [--item N] [--json]  the detail panel's view of a result
  dalibre-plugin download <dir> <text> [--item N] [--format EPUB] [-o DIR]
  dalibre-plugin identify <dir> [title] [--author A] [--isbn N] [--id type:value] [--language L] [--json]
                                                      what Fetch metadata shows for a book
  dalibre-plugin process <dir> <file> [--manual] [-o DIR]  run a file type plugin on a book's file
  dalibre-plugin pack <dir> [-o DIR]                  make <id>-<version>.dalibre-plugin
  dalibre-plugin install <package> [--yes]            install into Dalibre's plugins folder
  dalibre-plugin uninstall <id>
  dalibre-plugin installed                            list installed plugins

search, list, get, download, identify and process take --set key=value for the
plugin's settings (a file setting takes a file's path, copied into the plugin's
.data folder), and --filter key=value for its filters; the values saved in
Dalibre are used otherwise. The plugins folder is
$DALIBRE_PLUGINS, or Dalibre's in your configuration folder.
`

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		fmt.Print(usage)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cmds := map[string]func(context.Context, []string) error{
		"init": cmdInit, "check": cmdCheck, "search": cmdSearch, "download": cmdDownload,
		"collections": cmdCollections, "list": cmdListCollection, "get": cmdGet, "identify": cmdIdentify, "process": cmdProcess,
		"pack": cmdPack, "install": cmdInstall, "uninstall": cmdUninstall, "installed": cmdList,
	}
	cmd, ok := cmds[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err := cmd(ctx, os.Args[2:]); err != nil {
		report(err)
		os.Exit(1)
	}
}

// report prints an error, with what a plugin's author needs to fix it.
func report(err error) {
	var pe *plugin.Error
	if errors.As(err, &pe) {
		fmt.Fprintf(os.Stderr, "error (%s): %s\n", pe.Kind, pe.Error())
		if pe.Stack != "" {
			fmt.Fprintf(os.Stderr, "\n%s\n", pe.Stack)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "error:", err)
}

// parse parses flags wherever they are among the arguments, and returns
// the arguments that aren't flags, checking there are want of them.
func parse(fs *flag.FlagSet, args []string, want ...string) []string {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(pos) != len(want) {
		fmt.Fprintf(os.Stderr, "usage: dalibre-plugin %s %s\n", fs.Name(), "<"+strings.Join(want, "> <")+">")
		os.Exit(2)
	}
	return pos
}

type setFlags map[string]string

func (s setFlags) String() string { return "" }
func (s setFlags) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok {
		return errors.New("want key=value")
	}
	s[k] = val
	return nil
}

// load loads the plugin in dir with the settings saved in Dalibre and
// those given, logging to stderr.
func load(dir string, set setFlags) (*plugin.Plugin, error) {
	m, err := plugin.ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if st := settings.Load(settings.DefaultPath()).Plugins[m.ID]; st != nil {
		for k, v := range st.Values {
			values[k] = v
		}
	}
	// Secret settings are in the system's credential store, where the
	// app keeps them; --set is used instead when given.
	for _, st := range m.Settings {
		if _, given := set[st.Key]; st.Type != "secret" || given || values[st.Key] != "" {
			continue
		}
		if v, err := mygo.Secrets.Service(plugin.SecretService).Get(plugin.SecretKey(m.ID, st.Key)); err == nil {
			values[st.Key] = string(v)
		}
	}
	// The plugin's own folder is .data in its folder, which pack leaves
	// out. A file setting given with --set is a file here, copied in.
	own := filepath.Join(dir, ".data")
	for k, v := range set {
		i := slices.IndexFunc(m.Settings, func(s plugin.Setting) bool { return s.Key == k })
		if i < 0 {
			return nil, fmt.Errorf("--set %s: plugin.json has no setting %q", k, k)
		}
		if m.Settings[i].Type == "file" && v != "" {
			p, err := plugin.StoreSettingFile(own, k, v)
			if err != nil {
				return nil, fmt.Errorf("--set %s: %w", k, err)
			}
			v = p
		}
		values[k] = v
	}
	return plugin.Load(dir, plugin.Options{Settings: values, DataDir: own, Logf: func(f string, a ...any) {
		fmt.Fprintf(os.Stderr, f+"\n", a...)
	}})
}

func cmdInit(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	id := fs.String("id", "", "the plugin's id (default: the folder's name)")
	name := fs.String("name", "", "the plugin's name (default: the id)")
	dir := parse(fs, args, "dir")[0]
	if *id == "" {
		*id = strings.ToLower(filepath.Base(dir))
	}
	if *name == "" {
		*name = *id
	}
	if _, err := sdk.Init(dir, *id, *name); err != nil {
		return err
	}
	if _, err := plugin.ReadManifest(dir); err != nil {
		return fmt.Errorf("%w (is %q a valid id? pass --id)", err, *id)
	}
	fmt.Printf("Made %s. Edit plugin.json and main.js, then: dalibre-plugin search %s \"a title\"\n", dir, dir)
	return nil
}

func cmdCheck(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	dir := parse(fs, args, "dir")[0]
	p, err := plugin.Load(dir, plugin.Options{DataDir: filepath.Join(dir, ".data"), Logf: func(string, ...any) {}})
	if err != nil {
		return err
	}
	m := p.Manifest
	fmt.Printf("%s %s (%s), plugin API %d\n", m.Name, m.Version, m.ID, m.API)
	fmt.Printf("capabilities: %s\n", strings.Join(m.Capabilities.Names(), ", "))
	for _, f := range m.Filters() {
		var vals []string
		for _, o := range f.Options {
			vals = append(vals, fmt.Sprintf("%q", o.Value))
		}
		fmt.Printf("filter %s (%s): %s, default %q\n", f.Key, f.Type, strings.Join(vals, " "), f.Value(""))
	}
	if plugin.Reserved(m.ID) {
		fmt.Println("note: a reserved id; only a plugin shipped inside Dalibre may use it")
	}
	for _, fn := range []string{"search", "collections", "list", "get", "resolve", "identify"} {
		if p.Defines(fn) {
			fmt.Printf("exports %s\n", fn)
		}
	}
	fmt.Println("asks to:")
	for _, d := range m.Permissions.Describe() {
		fmt.Println("  " + d)
	}
	if m.Description == "" {
		fmt.Println("warning: no description; users see it before installing")
	}
	if m.Homepage == "" {
		fmt.Println("warning: no homepage")
	}
	fmt.Println("ok")
	return nil
}

func cmdSearch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	page := fs.Int("page", 1, "the page")
	cursor := fs.String("cursor", "", "the cursor a previous page returned")
	asJSON := fs.Bool("json", false, "print the results as JSON")
	set, filters := setFlags{}, setFlags{}
	fs.Var(set, "set", "a setting, as key=value (repeatable)")
	fs.Var(filters, "filter", "a filter's value, as key=value (repeatable)")
	pos := parse(fs, args, "dir", "text")
	p, err := load(pos[0], set)
	if err != nil {
		return err
	}
	if err := checkFilters(p, filters); err != nil {
		return err
	}
	res, err := p.Search(ctx, plugin.Query{Text: pos[1], Page: *page, Cursor: *cursor, Filters: filters})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	printPage(res)
	return nil
}

// checkFilters checks --filter values against the plugin's filters.
func checkFilters(p *plugin.Plugin, filters map[string]string) error {
	for k, v := range filters {
		i := slices.IndexFunc(p.Manifest.Filters(), func(f plugin.Filter) bool { return f.Key == k })
		if i < 0 {
			return fmt.Errorf("--filter %s: plugin.json declares no filter %q", k, k)
		}
		if f := p.Manifest.Filters()[i]; f.Value(v) != v {
			return fmt.Errorf("--filter %s=%s: not one of the filter's options", k, v)
		}
	}
	return nil
}

func cmdCollections(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("collections", flag.ExitOnError)
	set := setFlags{}
	fs.Var(set, "set", "a setting, as key=value (repeatable)")
	p, err := load(parse(fs, args, "dir")[0], set)
	if err != nil {
		return err
	}
	if !p.Manifest.HasCollections() {
		return errors.New("plugin.json doesn’t declare collections")
	}
	cs, err := p.Collections(ctx)
	if err != nil {
		return err
	}
	for _, c := range cs {
		fmt.Printf("%s\t%s\n", c.ID, c.Title)
	}
	return nil
}

func cmdListCollection(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	page := fs.Int("page", 1, "the page")
	cursor := fs.String("cursor", "", "the cursor a previous page returned")
	asJSON := fs.Bool("json", false, "print the results as JSON")
	set, filters := setFlags{}, setFlags{}
	fs.Var(set, "set", "a setting, as key=value (repeatable)")
	fs.Var(filters, "filter", "a filter's value, as key=value (repeatable)")
	pos := parse(fs, args, "dir", "collection")
	p, err := load(pos[0], set)
	if err != nil {
		return err
	}
	if err := checkFilters(p, filters); err != nil {
		return err
	}
	res, err := p.List(ctx, plugin.Query{Collection: pos[1], Page: *page, Cursor: *cursor, Filters: filters})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	printPage(res)
	return nil
}

func cmdGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	item := fs.Int("item", 1, "which result, from 1")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	set, filters := setFlags{}, setFlags{}
	fs.Var(set, "set", "a setting, as key=value (repeatable)")
	fs.Var(filters, "filter", "a filter's value, as key=value (repeatable)")
	pos := parse(fs, args, "dir", "text")
	p, err := load(pos[0], set)
	if err != nil {
		return err
	}
	res, err := p.Search(ctx, plugin.Query{Text: pos[1], Filters: filters})
	if err != nil {
		return err
	}
	if *item < 1 || *item > len(res.Items) {
		return fmt.Errorf("--item %d: search returned %d results", *item, len(res.Items))
	}
	r, err := p.Get(ctx, res.Items[*item-1])
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	printPage(plugin.Page{Items: []plugin.Result{r}})
	if r.URL != "" {
		fmt.Println("    page", r.URL)
	}
	for _, f := range r.Fields {
		fmt.Printf("    %s: %s\n", f.Label, f.Value)
	}
	if len(r.Tags) > 0 {
		fmt.Println("    tags:", strings.Join(r.Tags, "; "))
	}
	if r.Description != "" {
		fmt.Println("    " + r.Description)
	}
	return nil
}

func cmdIdentify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("identify", flag.ExitOnError)
	var authors, ids multiFlag
	fs.Var(&authors, "author", "an author (repeatable)")
	fs.Var(&ids, "id", "an identifier, as type:value (repeatable)")
	isbn := fs.String("isbn", "", "the book's ISBN")
	lang := fs.String("language", "", "the book's language, ISO 639")
	asJSON := fs.Bool("json", false, "print the candidates as JSON")
	set := setFlags{}
	fs.Var(set, "set", "a setting, as key=value (repeatable)")
	var pos []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			os.Exit(2)
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(pos) < 1 || len(pos) > 2 {
		fmt.Fprintln(os.Stderr, "usage: dalibre-plugin identify <dir> [title] [--author A] [--isbn N] [--id type:value]")
		os.Exit(2)
	}
	p, err := load(pos[0], set)
	if err != nil {
		return err
	}
	book := plugin.Book{Authors: authors, Identifiers: map[string]string{}, Language: *lang}
	if len(pos) == 2 {
		book.Title = pos[1]
	}
	for _, id := range ids {
		k, v, ok := strings.Cut(id, ":")
		if !ok {
			return fmt.Errorf("--id %q: want type:value", id)
		}
		book.Identifiers[k] = v
	}
	if *isbn != "" {
		book.Identifiers["isbn"] = *isbn
	}
	cands, err := p.Identify(ctx, book)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cands)
	}
	if len(cands) == 0 {
		fmt.Println("no candidates")
	}
	for i, r := range cands {
		fmt.Printf("%2d. %s", i+1, r.Title)
		if len(r.Authors) > 0 {
			fmt.Printf(" — %s", strings.Join(r.Authors, ", "))
		}
		fmt.Println()
		var line []string
		for _, s := range []string{r.Publisher, r.Pubdate, r.Language} {
			if s != "" {
				line = append(line, s)
			}
		}
		if r.Series != "" {
			line = append(line, "series "+r.Series)
		}
		if len(line) > 0 {
			fmt.Println("    " + strings.Join(line, " · "))
		}
		var idents []string
		for _, k := range slices.Sorted(maps.Keys(r.Identifiers)) {
			idents = append(idents, k+":"+r.Identifiers[k])
		}
		if len(idents) > 0 {
			fmt.Println("    " + strings.Join(idents, ", "))
		}
		if r.Cover != "" {
			fmt.Println("    cover", r.Cover)
		}
		if len(r.Tags) > 0 {
			fmt.Println("    tags:", strings.Join(r.Tags, "; "))
		}
	}
	return nil
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func printPage(res plugin.Page) {
	for i, r := range res.Items {
		fmt.Printf("%2d. %s", i+1, r.Title)
		if len(r.Authors) > 0 {
			fmt.Printf(" — %s", strings.Join(r.Authors, ", "))
		}
		fmt.Println()
		var formats []string
		for _, f := range r.Formats {
			s := f.Format
			if f.Size > 0 {
				s += fmt.Sprintf(" %.1f MB", float64(f.Size)/(1<<20))
			}
			formats = append(formats, s)
		}
		fmt.Printf("    id %s · %s", r.ID, strings.Join(formats, ", "))
		if r.Language != "" {
			fmt.Printf(" · %s", r.Language)
		}
		fmt.Println()
	}
	if len(res.Items) == 0 {
		fmt.Println("no results")
	}
	if res.HasMore {
		next := "--page N+1"
		if res.Cursor != "" {
			next = "--cursor " + res.Cursor
		}
		fmt.Printf("more: %s\n", next)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning: search returned", w)
	}
}

func cmdProcess(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("process", flag.ExitOnError)
	manual := fs.Bool("manual", false, `call it as the book menu does (reason "manual"), not as an import`)
	out := fs.String("o", ".", "the folder to save the new file in")
	set := setFlags{}
	fs.Var(set, "set", "a setting, as key=value (repeatable)")
	pos := parse(fs, args, "dir", "file")
	p, err := load(pos[0], set)
	if err != nil {
		return err
	}
	defer p.Close()
	reason := plugin.ReasonImport
	if *manual {
		reason = plugin.ReasonManual
	}
	start := time.Now()
	r, err := p.Process(ctx, pos[1], reason, nil)
	if err != nil {
		return err
	}
	defer r.Done()
	for _, line := range r.Report {
		fmt.Println("·", line)
	}
	if r.File == "" {
		fmt.Printf("left the file as it was (%s)\n", time.Since(start).Round(time.Millisecond))
		return nil
	}
	dst := filepath.Join(*out, filepath.Base(r.File))
	if same(dst, pos[1]) {
		dst = filepath.Join(*out, strings.TrimSuffix(filepath.Base(r.File), filepath.Ext(r.File))+" (processed)"+filepath.Ext(r.File))
	}
	data, err := os.ReadFile(r.File)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("saved %s (%s, %.1f MB, %s)\n", dst, r.Format, float64(len(data))/(1<<20), time.Since(start).Round(time.Millisecond))
	return nil
}

// same reports whether a and b are the same file's path.
func same(a, b string) bool {
	x, err1 := filepath.Abs(a)
	y, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && x == y
}

func cmdDownload(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	item := fs.Int("item", 1, "which result to download, from 1")
	format := fs.String("format", "", "the format (default: the result's first)")
	out := fs.String("o", ".", "the folder to save in")
	set := setFlags{}
	fs.Var(set, "set", "a setting, as key=value (repeatable)")
	pos := parse(fs, args, "dir", "text")
	p, err := load(pos[0], set)
	if err != nil {
		return err
	}
	res, err := p.Search(ctx, plugin.Query{Text: pos[1]})
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning: search returned", w)
	}
	if *item < 1 || *item > len(res.Items) {
		return fmt.Errorf("--item %d: search returned %d results", *item, len(res.Items))
	}
	r := res.Items[*item-1]
	if *format == "" {
		*format = r.Formats[0].Format
	}
	req, err := p.Resolve(ctx, r, *format)
	if err != nil {
		return err
	}
	fmt.Printf("%s (%s) from %s\n", r.Title, strings.ToUpper(*format), req.URL)
	path, err := p.Download(ctx, r, *format, *out, func(done, total int64) {
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r%.1f / %.1f MB", float64(done)/(1<<20), float64(total)/(1<<20))
		} else {
			fmt.Fprintf(os.Stderr, "\r%.1f MB", float64(done)/(1<<20))
		}
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	if _, err := p.Cover(ctx, r); err != nil {
		fmt.Fprintln(os.Stderr, "warning: cover:", err)
	}
	fmt.Println("saved", path)
	return nil
}

func cmdPack(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("pack", flag.ExitOnError)
	out := fs.String("o", ".", "the folder to write the package in")
	dir := parse(fs, args, "dir")[0]
	path, err := plugin.Pack(dir, *out)
	if err != nil {
		return err
	}
	fmt.Println("packed", path)
	return nil
}

func cmdInstall(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	yes := fs.Bool("yes", false, "grant the permissions without asking")
	archive := parse(fs, args, "package")[0]
	m, err := plugin.Inspect(archive)
	if err != nil {
		return err
	}
	s := settings.Load(settings.DefaultPath())
	st := s.Plugin(m.ID)
	have := plugin.Permissions{}
	if st.Granted != nil {
		have = plugin.Permissions{Hosts: st.Granted.Hosts, LocalNetwork: st.Granted.LocalNetwork, Library: st.Granted.Library}
	}
	if !have.Covers(m.Permissions) {
		fmt.Printf("%s %s", m.Name, m.Version)
		if m.Author != "" {
			fmt.Printf(" by %s", m.Author)
		}
		fmt.Printf(" (%s) asks to:\n", m.ID)
		for _, d := range m.Permissions.Describe() {
			fmt.Println("  " + d)
		}
		if !*yes {
			fmt.Print("Allow? [y/N] ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				return errors.New("not installed")
			}
		}
		st.Granted = &settings.Grant{Hosts: m.Permissions.Hosts, LocalNetwork: m.Permissions.LocalNetwork, Library: m.Permissions.Library}
	}
	if _, err := plugin.Install(archive, plugin.DefaultDir()); err != nil {
		return err
	}
	if err := s.Save(); err != nil {
		return err
	}
	fmt.Printf("installed %s %s in %s; restart Dalibre to use it\n", m.Name, m.Version, plugin.DefaultDir())
	return nil
}

func cmdUninstall(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	id := parse(fs, args, "id")[0]
	if err := plugin.Uninstall(plugin.DefaultDir(), id); err != nil {
		return err
	}
	s := settings.Load(settings.DefaultPath())
	delete(s.Plugins, id)
	return s.Save()
}

func cmdList(_ context.Context, args []string) error {
	parse(flag.NewFlagSet("list", flag.ExitOnError), args)
	root := plugin.DefaultDir()
	s := settings.Load(settings.DefaultPath())
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("no plugins in", root)
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m, err := plugin.ReadManifest(filepath.Join(root, e.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			fmt.Printf("%-24s error: %v\n", e.Name(), err)
			continue
		}
		state := "on"
		st := s.Plugins[m.ID]
		switch {
		case st != nil && st.Disabled:
			state = "off"
		case st == nil || st.Granted == nil ||
			!(plugin.Permissions{Hosts: st.Granted.Hosts, LocalNetwork: st.Granted.LocalNetwork, Library: st.Granted.Library}).Covers(m.Permissions):
			state = "waiting for permission"
		}
		fmt.Printf("%-24s %-10s %s (%s)\n", m.ID, m.Version, m.Name, state)
	}
	return nil
}
