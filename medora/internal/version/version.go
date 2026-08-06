package version

import (
	"os"
	"path/filepath"
	"strings"
)

var Version string

func Init(legalDir string) {
	Version = readVersion(filepath.Join(legalDir, "VERSION"))
}

func readVersion(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
