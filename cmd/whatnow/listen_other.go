//go:build !windows

package main

import (
	"errors"
	"syscall"
)

func addressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
