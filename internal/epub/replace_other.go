//go:build !windows

package epub

import "os"

// replaceFile puts src at dst, replacing dst. Rename does that on every
// system but Windows.
func replaceFile(src, dst string) error { return os.Rename(src, dst) }
