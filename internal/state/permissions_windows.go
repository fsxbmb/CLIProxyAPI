//go:build windows

package state

import (
	"fmt"
	"os"
	"os/exec"
)

// Windows uses ACLs instead of POSIX mode bits. Restrict each generated file
// and directory to the interactive user, while preserving inheritance for
// children created below a protected directory.
func securePrivate(path string, directory bool) error {
	user := os.Getenv("USERNAME")
	if user == "" {
		return fmt.Errorf("USERNAME is not set")
	}
	if domain := os.Getenv("USERDOMAIN"); domain != "" {
		user = domain + `\` + user
	}
	permission := user + ":F"
	if directory {
		permission = user + ":(OI)(CI)F"
	}
	if output, err := exec.Command("icacls", path, "/inheritance:r", "/grant:r", permission).CombinedOutput(); err != nil {
		return fmt.Errorf("icacls %s: %w (%s)", path, err, output)
	}
	return nil
}

func validatePrivate(paths Paths) error {
	for _, path := range []string{paths.Root, paths.AuthDir, paths.SecretsFile, paths.ConfigFile} {
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	return nil
}
