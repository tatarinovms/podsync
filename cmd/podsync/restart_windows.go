//go:build windows

package main

import "errors"

func restartCurrentProcess() error {
	return errors.New("automatic restart after an update is unavailable on Windows")
}
