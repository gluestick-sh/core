package install

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gluestick-sh/core/apps"
	"github.com/gluestick-sh/core/engine/internal/runtime"
)

// ResolveLocalSevenZip returns an existing 7-Zip binary under glue root or on PATH.
// It never downloads; callers bootstrap only when this returns empty.
func ResolveLocalSevenZip(glueRoot string) string {
	var candidates []string
	if glueRoot != "" {
		candidates = append(candidates,
			filepath.Join(glueRoot, "bin", "7z.exe"),
			filepath.Join(glueRoot, "bin", "7zr.exe"),
			filepath.Join(glueRoot, "bin", "7za.exe"),
		)
		if installDir, _, err := apps.CurrentInstalledPath(glueRoot, "7zip"); err == nil {
			candidates = append(candidates, filepath.Join(installDir, "7z.exe"))
		}
	}
	if p, err := exec.LookPath("7z"); err == nil {
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath("7z.exe"); err == nil {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// SetSevenZipFromLocal configures the extractor from glue bin or PATH when available.
func SetSevenZipFromLocal(e *runtime.Engine) bool {
	if e == nil || e.Extractor == nil {
		return false
	}
	if path := e.Extractor.SevenZipPath(); path != "" {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return true
		}
	}
	if path := ResolveLocalSevenZip(e.Config.RootDir); path != "" {
		e.Extractor.Set7zPath(path)
		return true
	}
	return false
}
