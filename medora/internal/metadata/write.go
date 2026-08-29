package metadata

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
)

// WriteBesideDir writes filename into dir from r (creates dir if needed).
func WriteBesideDir(dir, filename string, r io.Reader) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(filename))
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		_ = os.Remove(dst)
		return "", err
	}
	return dst, nil
}

// WriteBytesBesideDir writes data into dir/filename.
func WriteBytesBesideDir(dir, filename string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(filename))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

func WriteSeasonNFO(path string, n SeasonNFO) error {
	b, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), b...), 0o644)
}

func WriteEpisodeNFO(path string, n EpisodeNFO) error {
	b, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), b...), 0o644)
}
