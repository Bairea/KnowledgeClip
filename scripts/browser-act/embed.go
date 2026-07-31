package browseract

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed all:*
var embeddedScripts embed.FS

var (
	extractOnce sync.Once
	extractDir  string
	extractErr  error
)

// ExtractTo extracts the embedded JS snippets to a temp directory and returns
// the path. Extraction runs only once per process; later calls reuse the cache.
// Note: go:embed excludes files whose names start with "." or "_", so the
// shared lib is named lib.js (not _lib.js) to be embeddable.
func ExtractTo() (string, error) {
	extractOnce.Do(func() {
		extractDir, extractErr = extractLocked()
	})
	return extractDir, extractErr
}

// extractLocked performs the actual extraction.
func extractLocked() (string, error) {
	tempDir, err := os.MkdirTemp("", "kc-scripts-*")
	if err != nil {
		return "", err
	}

	err = fs.WalkDir(embeddedScripts, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Only site JS snippets are needed at runtime. Skip embed.go itself,
		// setup scripts, and docs.
		if !strings.HasSuffix(path, ".js") {
			return nil
		}
		data, err := embeddedScripts.ReadFile(path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(tempDir, path)
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0644)
	})

	if err != nil {
		os.RemoveAll(tempDir)
		return "", err
	}

	log.Printf("[browser-act] extracted embedded scripts to: %s", tempDir)
	return tempDir, nil
}
