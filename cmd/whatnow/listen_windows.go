package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

func addressInUse(err error) bool {
	// Winsock uses WSAEADDRINUSE, not the POSIX EADDRINUSE value.
	return errors.Is(err, windows.WSAEADDRINUSE)
}
