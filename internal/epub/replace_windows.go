//go:build windows

package epub

import "golang.org/x/sys/windows"

// replaceFile puts src at dst, replacing dst. Windows' os.Rename refuses an
// existing dst, and a book is usually saved over the file it was opened
// from, so MoveFileEx does the replace.
func replaceFile(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
