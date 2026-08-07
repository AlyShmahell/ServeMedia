package install

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alyshmahell/medora-plugin-sdk/manifest"
)

const MaxZipBytes = 64 << 20 // 64 MiB

// ExtractZip validates and extracts a plugin zip to destDir (must not exist or be replaced by caller).
func ExtractZip(r io.ReaderAt, size int64, destDir string) (manifest.Plugin, error) {
	if size <= 0 || size > MaxZipBytes {
		return manifest.Plugin{}, fmt.Errorf("plugin archive size invalid")
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return manifest.Plugin{}, fmt.Errorf("read zip: %w", err)
	}
	var plug manifest.Plugin
	foundManifest := false
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		if strings.Contains(f.Name, "..") {
			return manifest.Plugin{}, fmt.Errorf("invalid zip path %q", f.Name)
		}
		dest, err := manifest.SafeExtractPath(destDir, base)
		if err != nil {
			return manifest.Plugin{}, err
		}
		if err := extractFile(f, dest); err != nil {
			return manifest.Plugin{}, err
		}
		if base == "plugin.yaml" {
			foundManifest = true
			plug, err = manifest.ParseFile(dest)
			if err != nil {
				return manifest.Plugin{}, err
			}
		}
	}
	if !foundManifest {
		return manifest.Plugin{}, fmt.Errorf("plugin.yaml missing from archive")
	}
	binPath := filepath.Join(destDir, plug.Binary)
	if st, err := os.Stat(binPath); err != nil || st.IsDir() {
		return manifest.Plugin{}, fmt.Errorf("binary %q missing from archive", plug.Binary)
	}
	_ = os.Chmod(binPath, 0o755)
	settingsPath := filepath.Join(destDir, "settings.html")
	if st, err := os.Stat(settingsPath); err != nil || st.IsDir() {
		return manifest.Plugin{}, fmt.Errorf("settings.html missing from archive")
	}
	return plug, nil
}

func extractFile(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// ReadManifestVersion reads plugin.yaml from an installed directory.
func ReadManifestVersion(dir string) (manifest.Plugin, error) {
	return manifest.ParseFile(filepath.Join(dir, "plugin.yaml"))
}
