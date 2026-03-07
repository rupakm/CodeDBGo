// Package paths provides canonical path resolution for CodeDB data locations.
// Follows XDG Base Directory Specification, consistent with ox CLI conventions.
package paths

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	homeDir     string
	homeDirOnce sync.Once
)

func getHomeDir() string {
	homeDirOnce.Do(func() {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			homeDir = ""
		}
	})
	return homeDir
}

func xdgDataHome() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	return filepath.Join(getHomeDir(), ".local", "share")
}

// DataDir returns the persistent data directory for CodeDB.
// Default: ~/.local/share/sageox/codedb
func DataDir() string {
	return filepath.Join(xdgDataHome(), "sageox", "codedb")
}
