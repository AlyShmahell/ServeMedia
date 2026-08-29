package version

import (
	"os"
	"path/filepath"
	"strings"
)

var Version string

func Init(exeDir string) {
	Version = readVersion(filepath.Join(exeDir, "VERSION"))
	if Version == "" {
		Version = "0.0.1"
	}
}

func readVersion(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
