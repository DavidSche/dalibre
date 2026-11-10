// Package plugin runs Dalibre's plugins, which anyone may write: folders
// holding a plugin.json manifest and a JavaScript file, run in an
// embedded interpreter (no cgo, nothing for the user to install).
//
// Plugins are untrusted. A script has no file, process or network access
// of its own: it reaches the network only through the host's fetch, only
// to the hosts its manifest lists and the user granted, and never to the
// user's own machine or network unless granted that too. Every call has a
// deadline and a request budget, and what a plugin returns is checked
// field by field before the host uses it.
//
// A plugin declares what it adds to Dalibre in its manifest's
// capabilities. API version 1 has three: "bookSource" ("source" in the
// array form), a plugin that searches and browses a catalogue and says
// where a book's file is (the host downloads it), shown in the Get books
// page; "metadata", a plugin that looks up a book's details for the
// Edit metadata dialog's Fetch metadata; and "fileType", a plugin that
// works on books' files as they are added or when the user asks. Plugins
// never draw UI.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// APIVersion is the plugin API this Dalibre implements. Within a version
// the API only grows: new optional fields and functions, never a changed
// or removed one. A manifest asking for a newer version is refused.
const APIVersion = 1

// Icons a plugin may pick for itself, from Dalibre's own set: never an
// image, so a plugin can't dress up as another app.
var Icons = []string{"globe", "library", "book-open", "database", "network", "building", "bookmark", "layers"}

// Filter types a book source may declare.
const (
	FilterSelect   = "select"   // one of options
	FilterLanguage = "language" // one of options, which are ISO 639 codes; Dalibre names them
)

// Limits on what a plugin may be.
const (
	MaxScriptSize = 4 << 20
	MaxSettings   = 32
)

// Manifest is a plugin's plugin.json.
type Manifest struct {
	Schema      string `json:"$schema,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"` // semver, as "1.2.0"
	API         int    `json:"api"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	License     string `json:"license,omitempty"`
	Main        string `json:"main,omitempty"` // main.js when empty
	// Icon names the plugin's icon in Dalibre's set (Icons); "globe" when
	// empty.
	Icon         string       `json:"icon,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
	Permissions  Permissions  `json:"permissions"`
	Settings     []Setting    `json:"settings,omitempty"`
}

// Permissions are what a plugin may reach. The user grants them when the
// plugin is installed, and again when an update asks for more.
type Permissions struct {
	// Hosts the plugin may fetch and download from, redirects included,
	// as "example.com" or "*.example.com" (any subdomain, not the domain).
	Hosts []string `json:"hosts"`
	// LocalNetwork lets the plugin reach this computer and the local
	// network (loopback, private and link-local addresses), as an OPDS
	// server at home. Without it those addresses are refused, whatever
	// a listed host resolves to.
	LocalNetwork bool `json:"localNetwork,omitempty"`
	// Library is what the plugin sees of the user's library: LibraryRead
	// lets it see the books it is asked about (their title, authors,
	// identifiers and other details), which a metadata plugin and a file
	// type plugin's hooks need; LibraryWrite also lets its hooks change
	// those details, without asking each time.
	Library string `json:"library,omitempty"`
}

// Library access a plugin may ask for.
const (
	LibraryRead  = "read"
	LibraryWrite = "write"
)

// CanRead reports whether the permissions let the plugin see books.
func (p Permissions) CanRead() bool { return p.Library == LibraryRead || p.Library == LibraryWrite }

// Setting is a value the user gives a plugin, as an API key.
type Setting struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Type        string `json:"type,omitempty"` // "text" (default), "secret" or "file"
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Capabilities are what a plugin adds to Dalibre. In plugin.json they are
// an object, {"bookSource": {...}}, or, as API 1 began, an array of names:
// ["source"].
type Capabilities struct {
	BookSource *BookSource `json:"bookSource,omitempty"`
	Metadata   *Metadata   `json:"metadata,omitempty"`
	FileType   *FileType   `json:"fileType,omitempty"`
}

// FileType works on books' files, as calibre's file type plugins, with
// their hooks:
//
//   - process(file), as calibre's on_import, gets a copy of a book's file
//     and returns the path of a new one, or null to leave it. Dalibre
//     calls it for every file it adds (unless OnImport is false), and
//     when the user runs the plugin on books from the book menu.
//   - postimport(event) after a file was added to a book, as on_postimport;
//     postadd(event) after a book was added, as on_postadd; postdelete(event)
//     after a format was removed, as on_postdelete. They may return
//     changes to the book's metadata, which Dalibre writes (with
//     permissions.library "write"), where calibre's get its database.
type FileType struct {
	// Title names what the plugin does, in the book menu: "Clean up
	// EPUB". The plugin's name when empty.
	Title string `json:"title,omitempty"`
	// OnImport, when false, keeps the plugin from running on the files
	// Dalibre adds: it runs only when the user asks. True when absent.
	OnImport *bool `json:"onImport,omitempty"`
	// OnPostImport, OnPostAdd and OnPostDelete call postimport, postadd
	// and postdelete.
	OnPostImport bool `json:"onPostImport,omitempty"`
	OnPostAdd    bool `json:"onPostAdd,omitempty"`
	OnPostDelete bool `json:"onPostDelete,omitempty"`
}

// The hooks a file type plugin may export, after what calibre's do.
const (
	HookPostImport = "postimport"
	HookPostAdd    = "postadd"
	HookPostDelete = "postdelete"
)

// Hooks are the hooks the plugin asks to be called for.
func (m *Manifest) Hooks() []string {
	ft := m.Capabilities.FileType
	if ft == nil {
		return nil
	}
	var out []string
	if ft.OnPostImport {
		out = append(out, HookPostImport)
	}
	if ft.OnPostAdd {
		out = append(out, HookPostAdd)
	}
	if ft.OnPostDelete {
		out = append(out, HookPostDelete)
	}
	return out
}

// HasHook reports whether the plugin asks for hook.
func (m *Manifest) HasHook(hook string) bool { return slices.Contains(m.Hooks(), hook) }

// Metadata looks up books' details for the Edit metadata dialog. The
// plugin exports identify(book), which returns candidates, best first;
// Dalibre shows them next to the book's values and writes only the
// fields the user picks. It needs permissions.library "read".
type Metadata struct {
	// Fields are those the source can fill, as MetadataFields names them;
	// shown to the user. Empty means it doesn't say.
	Fields []string `json:"fields,omitempty"`
}

// MetadataFields are the fields a metadata source may declare.
var MetadataFields = []string{
	"title", "authors", "series", "tags", "publisher", "pubdate", "language", "identifiers", "cover", "description",
}

// BookSource is a catalogue Dalibre's Get books page searches, browses and
// downloads from. The plugin exports search (required), collections and
// list (with Collections), get and resolve.
type BookSource struct {
	// Filters narrow a search; Dalibre draws them and passes their values
	// in Query.Filters.
	Filters []Filter `json:"filters,omitempty"`
	// Collections says the source has collections to browse while the
	// search box is empty ("Most popular", "New"): it exports collections
	// and list.
	Collections bool `json:"collections,omitempty"`
}

// Filter is a control the Get books page shows under the search box.
type Filter struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	// Type is FilterSelect or FilterLanguage.
	Type    string   `json:"type"`
	Options []Option `json:"options"`
	// Default is the option chosen at first; the first option when empty.
	Default string `json:"default,omitempty"`
}

// Option is one value of a Filter. A language option's title may be left
// out: Dalibre names the language.
type Option struct {
	Value string `json:"value"`
	Title string `json:"title,omitempty"`
}

// Names lists the capabilities, as plugin.json names them.
func (c Capabilities) Names() []string {
	var out []string
	if c.BookSource != nil {
		out = append(out, "bookSource")
	}
	if c.Metadata != nil {
		out = append(out, "metadata")
	}
	if c.FileType != nil {
		out = append(out, "fileType")
	}
	return out
}

func (c *Capabilities) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '[' {
		var names []string
		if err := json.Unmarshal(data, &names); err != nil {
			return err
		}
		for _, n := range names {
			switch n {
			case "source", "bookSource":
				if c.BookSource == nil {
					c.BookSource = &BookSource{}
				}
			case "metadata":
				if c.Metadata == nil {
					c.Metadata = &Metadata{}
				}
			default:
				return fmt.Errorf("unknown capability %q", n)
			}
		}
		return nil
	}
	type plain Capabilities
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p plain
	if err := dec.Decode(&p); err != nil {
		if f, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			return fmt.Errorf("unknown capability %s", f)
		}
		return err
	}
	*c = Capabilities(p)
	return nil
}

// IsBookSource reports whether the plugin is a book source.
func (m *Manifest) IsBookSource() bool { return m.Capabilities.BookSource != nil }

// IsMetadataSource reports whether the plugin looks up books' metadata.
func (m *Manifest) IsMetadataSource() bool { return m.Capabilities.Metadata != nil }

// IsFileType reports whether the plugin works on books' files.
func (m *Manifest) IsFileType() bool { return m.Capabilities.FileType != nil }

// RunsOnImport reports whether the plugin processes the files Dalibre adds.
func (m *Manifest) RunsOnImport() bool {
	ft := m.Capabilities.FileType
	return ft != nil && (ft.OnImport == nil || *ft.OnImport)
}

// ActionTitle is what the book menu calls running the plugin: its
// fileType title, else its name.
func (m *Manifest) ActionTitle() string {
	if ft := m.Capabilities.FileType; ft != nil && strings.TrimSpace(ft.Title) != "" {
		return strings.TrimSpace(ft.Title)
	}
	return m.Name
}

// Filters are the book source's filters; none when it isn't one.
func (m *Manifest) Filters() []Filter {
	if m.Capabilities.BookSource == nil {
		return nil
	}
	return m.Capabilities.BookSource.Filters
}

// HasCollections reports whether the book source has collections to browse.
func (m *Manifest) HasCollections() bool {
	return m.Capabilities.BookSource != nil && m.Capabilities.BookSource.Collections
}

// IconName is the plugin's icon, "globe" by default.
func (m *Manifest) IconName() string {
	if m.Icon == "" {
		return "globe"
	}
	return m.Icon
}

// Reserved reports whether id is kept for plugins that ship with Dalibre.
func Reserved(id string) bool {
	return slices.ContainsFunc(reservedPrefix, func(p string) bool { return id == p || strings.HasPrefix(id, p+".") })
}

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	semverPattern  = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	keyPattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	hostPattern    = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
	reservedPrefix = []string{"dalibre"}
)

// ReadManifest reads and checks dir/plugin.json.
func ReadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, err
	}
	return ParseManifest(data)
}

// ParseManifest parses and checks a plugin.json. Unknown fields are
// errors, so a misspelt one is caught rather than ignored.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	if err := m.check(); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	return &m, nil
}

func (m *Manifest) check() error {
	switch {
	case len(m.ID) > 64 || !idPattern.MatchString(m.ID):
		return fmt.Errorf("id %q must be lowercase letters, digits and '-', in dot-separated parts, as \"gutenberg\" or \"com.example.books\"", m.ID)
	case strings.TrimSpace(m.Name) == "" || len(m.Name) > 64:
		return errors.New("name must be 1 to 64 characters")
	case !semverPattern.MatchString(m.Version):
		return fmt.Errorf("version %q must be a semantic version, as \"1.0.0\"", m.Version)
	case m.API < 1:
		return errors.New("api is missing; this Dalibre implements api 1")
	case m.API > APIVersion:
		return fmt.Errorf("needs plugin API %d; this Dalibre has %d, update Dalibre", m.API, APIVersion)
	case len(m.Capabilities.Names()) == 0:
		return errors.New("capabilities are missing")
	case m.Icon != "" && !slices.Contains(Icons, m.Icon):
		return fmt.Errorf("icon %q isn’t one of %s", m.Icon, strings.Join(Icons, ", "))
	case len(m.Settings) > MaxSettings:
		return fmt.Errorf("at most %d settings", MaxSettings)
	}
	if m.Main == "" {
		m.Main = "main.js"
	}
	if main := filepath.ToSlash(m.Main); filepath.IsAbs(m.Main) || strings.HasPrefix(main, "/") ||
		slices.Contains(strings.Split(main, "/"), "..") || !slices.Contains([]string{".js", ".mjs", ".ts", ".mts"}, path.Ext(main)) {
		return fmt.Errorf("main %q must be a .js or .ts file inside the plugin's folder", m.Main)
	}
	if m.IsBookSource() {
		if len(m.Permissions.Hosts) == 0 {
			return errors.New("a book source must list the hosts it reaches in permissions.hosts")
		}
		if err := checkFilters(m.Capabilities.BookSource.Filters); err != nil {
			return fmt.Errorf("capabilities.bookSource.%w", err)
		}
	}
	if md := m.Capabilities.Metadata; md != nil {
		if len(m.Permissions.Hosts) == 0 {
			return errors.New("a metadata source must list the hosts it reaches in permissions.hosts")
		}
		if !m.Permissions.CanRead() {
			return errors.New(`a metadata source sees the books it looks up: it needs permissions.library "read"`)
		}
		for _, f := range md.Fields {
			if !slices.Contains(MetadataFields, f) {
				return fmt.Errorf("capabilities.metadata.fields: %q isn’t one of %s", f, strings.Join(MetadataFields, ", "))
			}
		}
	}
	if ft := m.Capabilities.FileType; ft != nil && len([]rune(ft.Title)) > 64 {
		return errors.New("capabilities.fileType.title is over 64 characters")
	}
	if len(m.Hooks()) > 0 && !m.Permissions.CanRead() {
		return errors.New(`a file type plugin's hooks see the books they run for: they need permissions.library "read" (or "write" to change them)`)
	}
	if l := m.Permissions.Library; l != "" && l != LibraryRead && l != LibraryWrite {
		return fmt.Errorf(`permissions.library %q must be "read" or "write" (or left out)`, l)
	}
	for _, h := range m.Permissions.Hosts {
		if !hostPattern.MatchString(h) || !strings.Contains(strings.TrimPrefix(h, "*."), ".") && h != "localhost" {
			return fmt.Errorf("host %q must be a lowercase host name, as \"example.com\" or \"*.example.com\"", h)
		}
	}
	keys := map[string]bool{}
	for _, s := range m.Settings {
		switch {
		case !keyPattern.MatchString(s.Key):
			return fmt.Errorf("setting key %q must be letters, digits and '_', starting with a letter", s.Key)
		case keys[s.Key]:
			return fmt.Errorf("setting %q is defined twice", s.Key)
		case strings.TrimSpace(s.Title) == "":
			return fmt.Errorf("setting %q has no title", s.Key)
		case s.Type != "" && s.Type != "text" && s.Type != "secret" && s.Type != "file":
			return fmt.Errorf("setting %q: type must be \"text\", \"secret\" or \"file\"", s.Key)
		}
		keys[s.Key] = true
	}
	return nil
}

// MaxFilters and MaxOptions bound a book source's filters.
const (
	MaxFilters = 4
	MaxOptions = 64
)

func checkFilters(fs []Filter) error {
	if len(fs) > MaxFilters {
		return fmt.Errorf("filters: at most %d", MaxFilters)
	}
	keys := map[string]bool{}
	for i := range fs {
		f := &fs[i]
		at := fmt.Sprintf("filters[%d]", i)
		switch {
		case !keyPattern.MatchString(f.Key):
			return fmt.Errorf("%s.key %q must be letters, digits and '_', starting with a letter", at, f.Key)
		case keys[f.Key]:
			return fmt.Errorf("%s.key %q is used twice", at, f.Key)
		case strings.TrimSpace(f.Title) == "":
			return fmt.Errorf("%s has no title", at)
		case f.Type != FilterSelect && f.Type != FilterLanguage:
			return fmt.Errorf("%s.type must be %q or %q", at, FilterSelect, FilterLanguage)
		case len(f.Options) == 0 || len(f.Options) > MaxOptions:
			return fmt.Errorf("%s needs 1 to %d options", at, MaxOptions)
		}
		keys[f.Key] = true
		vals := map[string]bool{}
		for j, o := range f.Options {
			if vals[o.Value] {
				return fmt.Errorf("%s.options[%d].value %q is used twice", at, j, o.Value)
			}
			vals[o.Value] = true
			if f.Type == FilterSelect && strings.TrimSpace(o.Title) == "" {
				return fmt.Errorf("%s.options[%d] has no title", at, j)
			}
		}
		if f.Default != "" && !vals[f.Default] {
			return fmt.Errorf("%s.default %q isn’t one of its options", at, f.Default)
		}
	}
	return nil
}

// Value is the filter's option for v: v when it is one, else the default.
func (f Filter) Value(v string) string {
	for _, o := range f.Options {
		if o.Value == v {
			return v
		}
	}
	if f.Default != "" {
		return f.Default
	}
	return f.Options[0].Value
}

// allows reports whether the permissions let the plugin reach host.
func (p *Permissions) allows(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range p.Hosts {
		if suffix, ok := strings.CutPrefix(h, "*"); ok {
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		} else if host == h {
			return true
		}
	}
	return false
}

// Covers reports whether p, as granted, allows everything want asks
// for. An update asking for more than was granted needs the user again.
func (p Permissions) Covers(want Permissions) bool {
	if want.LocalNetwork && !p.LocalNetwork {
		return false
	}
	switch want.Library {
	case LibraryRead:
		if !p.CanRead() {
			return false
		}
	case LibraryWrite:
		if p.Library != LibraryWrite {
			return false
		}
	}
	for _, h := range want.Hosts {
		if !slices.Contains(p.Hosts, h) {
			return false
		}
	}
	return true
}

// Describe lists the permissions as the user is asked to grant them.
func (p Permissions) Describe() []string {
	var out []string
	for _, h := range p.Hosts {
		if d, ok := strings.CutPrefix(h, "*."); ok {
			out = append(out, "Connect to any subdomain of "+d)
		} else {
			out = append(out, "Connect to "+h)
		}
	}
	if p.LocalNetwork {
		out = append(out, "Connect to this computer and your local network")
	}
	switch p.Library {
	case LibraryRead:
		out = append(out, "See the title, authors and identifiers of books you look up with it")
	case LibraryWrite:
		out = append(out, "See and change the details of the books it runs for")
	}
	return out
}

// SecretService is the service in the system's credential store under
// which Dalibre keeps plugins' secret settings: the app's identifier.
const SecretService = "org.randynamic.dalibre"

// SecretKey is the credential store's key for secret setting key of
// plugin id.
func SecretKey(id, key string) string { return "plugin-" + id + "-" + key }
