// Package plugins holds the plugins that ship inside Dalibre. They are
// plugins like any other, run in the same sandbox, with ids under the
// reserved "dalibre." prefix; Dalibre grants them what they ask for, as
// it vouches for them.
package plugins

import (
	"embed"
	"io/fs"
)

//go:embed gutenberg/plugin.json gutenberg/main.js googlebooks/plugin.json googlebooks/main.js neodb/plugin.json neodb/main.js
var files embed.FS

// Builtin returns each built-in plugin's files, plugin.json at the root.
func Builtin() []fs.FS {
	var out []fs.FS
	entries, _ := fs.ReadDir(files, ".")
	for _, e := range entries {
		if sub, err := fs.Sub(files, e.Name()); err == nil && e.IsDir() {
			out = append(out, sub)
		}
	}
	return out
}
