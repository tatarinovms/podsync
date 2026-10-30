//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

func restartCurrentProcess() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable for restart: %w", err)
	}
	return syscall.Exec(executable, os.Args, os.Environ())
}
