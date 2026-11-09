//go:build windows

package app

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// addressInUse reports whether a listen failed because another program has
// the port. Windows reports WSAEADDRINUSE from its own table, which
// errors.Is(err, syscall.EADDRINUSE) does not see through net.OpError.
func addressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, windows.WSAEADDRINUSE)
}
