//go:build windows

package main

func getDiskSpace(path string) (total uint64, free uint64, used uint64, err error) {
	// Not implemented on Windows without cgo/win32 dll calls
	return 0, 0, 0, nil
}
