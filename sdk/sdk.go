// Package sdk holds what plugin authors are given: the API's type
// declarations, the manifest's JSON Schema and a starter plugin.
package sdk

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Files are dalibre.d.ts, plugin.schema.json and template/.
//
//go:embed dalibre.d.ts plugin.schema.json template
var Files embed.FS

// Init starts a plugin in dir, which must be empty or not there yet, from
// the template: its plugin.json with id and name, main.js, and the type
// declarations and schema editors check them with. It returns the names
// of the files it wrote.
func Init(dir, id, name string) ([]string, error) {
	if entries, _ := os.ReadDir(dir); len(entries) > 0 {
		return nil, fmt.Errorf("%s isn’t empty", dir)
	}
	files := map[string]string{"dalibre.d.ts": "dalibre.d.ts", "plugin.schema.json": "plugin.schema.json"}
	entries, err := fs.ReadDir(Files, "template")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		files[e.Name()] = "template/" + e.Name()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var names []string
	for dst, src := range files {
		data, err := Files.ReadFile(src)
		if err != nil {
			return nil, err
		}
		text := strings.NewReplacer("{{ID}}", id, "{{NAME}}", name).Replace(string(data))
		if err := os.WriteFile(filepath.Join(dir, dst), []byte(text), 0o644); err != nil {
			return nil, err
		}
		names = append(names, dst)
	}
	slices.Sort(names)
	return names, nil
}
