package browseract

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

//go:embed all:*
var embeddedScripts embed.FS

// ExtractTo extracts embedded scripts to a temp directory and returns the path.
func ExtractTo() (string, error) {
	tempDir, err := os.MkdirTemp("", "kc-scripts-*")
	if err != nil {
		return "", err
	}

	err = fs.WalkDir(embeddedScripts, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the embed.go file itself and other non-JS files
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(tempDir, path), 0755)
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
