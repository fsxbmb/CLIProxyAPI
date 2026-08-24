//go:build !windows

package state

import (
	"fmt"
	"io/fs"
	"os"
)

func securePrivate(path string, directory bool) error {
	mode := fs.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	return os.Chmod(path, mode)
}

func validatePrivate(paths Paths) error {
	checks := []struct {
		path string
		want fs.FileMode
	}{
		{paths.Root, 0o700},
		{paths.AuthDir, 0o700},
		{paths.SecretsFile, 0o600},
		{paths.ConfigFile, 0o600},
	}
	for _, check := range checks {
		info, err := os.Stat(check.path)
		if err != nil {
			return fmt.Errorf("stat %s: %w", check.path, err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%s permissions are %04o; expected no group/other access", check.path, info.Mode().Perm())
		}
	}
	return nil
}
