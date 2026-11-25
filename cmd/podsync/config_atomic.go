package main

import (
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml"
)

// writeConfigTree replaces the config only after a complete, valid TOML file
// has been written and synced in the same directory, preserving the mode of
// the existing file.
func writeConfigTree(path string, tree *toml.Tree) error {
	return writeConfigTreeMode(path, tree, 0)
}

// writeConfigTreeMode is writeConfigTree with an optional forced file mode.
// A zero mode preserves the current file mode. Callers that persist secrets
// pass 0600 so atomic replacement cannot widen the file permissions.
func writeConfigTreeMode(path string, tree *toml.Tree, forceMode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".podsync-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()

	mode := forceMode
	if mode == 0 {
		mode = 0600
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := tree.WriteTo(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if _, err := toml.LoadFile(f.Name()); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	if dirFile, openErr := os.Open(dir); openErr == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
}
