// ABOUTME: Resolves filesystem paths to editor template and static asset directories.
// ABOUTME: Supports both development (source tree) and installed binary layouts.
package web

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func resolveEditorAssetDirs() (templateDir, staticDir string, err error) {
	candidates := [][2]string{
		{"editor/templates", "editor/static"},
		{"../editor/templates", "../editor/static"},
	}

	if _, thisFile, _, ok := runtime.Caller(0); ok {
		base := filepath.Dir(thisFile)
		candidates = append(candidates,
			[2]string{filepath.Join(base, "..", "editor", "templates"), filepath.Join(base, "..", "editor", "static")},
		)
	}

	for _, c := range candidates {
		if dirExists(c[0]) && dirExists(c[1]) {
			return c[0], c[1], nil
		}
	}

	return "", "", fmt.Errorf("editor templates/static directories not found")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
