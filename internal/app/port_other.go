//go:build !windows

package app

import (
	"errors"
	"syscall"
)

// addressInUse reports whether a listen failed because another program has
// the port.
func addressInUse(err error) bool { return errors.Is(err, syscall.EADDRINUSE) }
