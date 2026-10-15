package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gluestick-sh/core/engine/internal/runtime"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/shim"
	"github.com/gluestick-sh/core/verbose"
)

// ShimNameForBin returns the PATH command name for a manifest bin entry.
func ShimNameForBin(exePath, alias string) string {
	return shimNameForBin(exePath, alias)
}

// CreatePackageShims registers PATH shims for m. Names already provided by another
// installed package are skipped because the shims directory is a flat namespace.
func CreatePackageShims(shimMgr *shim.Manager, shimsMetaDir, pkgName, installDir, shimDir string, m *manifest.Manifest) error {
	return createPackageShims(shimMgr, shimsMetaDir, pkgName, installDir, shimDir, m)
}

func createPackageShims(shimMgr *shim.Manager, shimsMetaDir, pkgName, installDir, shimDir string, m *manifest.Manifest) error {
	if shimDir == "" {
		shimDir = installDir
	}
	c := &shimCreator{
		shimMgr:      shimMgr,
		shimsMetaDir: shimsMetaDir,
		pkgName:      pkgName,
		shimEnv:      ShimEnvForPackage(m, installDir, ""),
	}
	createManifestBinShims(c, installDir, shimDir, m)
	createEnvAddPathShims(c, installDir, shimDir, m)
	return c.firstErr
}

// shimCreator creates PATH shims for one package, remembering only the first
// failure so remaining entries are still attempted.
type shimCreator struct {
	shimMgr      *shim.Manager
	shimsMetaDir string
	pkgName      string
	shimEnv      map[string]string
	firstErr     error
}

func (c *shimCreator) recordErr(err error) {
	if err != nil && c.firstErr == nil {
		c.firstErr = err
	}
}

// tryCreate creates one shim unless another package already owns the name; the
// shims directory is a flat namespace and cannot hold duplicates.
func (c *shimCreator) tryCreate(shimName, shimTarget, label, fromDir string, extraArgs []string) {
	if owner, conflict := shim.OwnedByOtherPackage(c.shimsMetaDir, shimName, c.pkgName); conflict {
		verbose.Progressf("    skipped shim %s (already provided by %s)\n", shimName, owner)
		return
	}
	opts := shim.CreateOpts{Args: extraArgs, Env: c.shimEnv}
	if err := c.shimMgr.Create(shimName, shimTarget, opts); err != nil {
		verbose.Progressf("    %s failed to create shim for %s: %v\n", verbose.FailedMark(), label, err)
		c.recordErr(fmt.Errorf("shim %s: %w", shimName, err))
		return
	}
	if fromDir != "" {
		verbose.Progressf("    %s %s (from %s)\n", verbose.SuccessMark(), shimName, fromDir)
	} else {
		verbose.Progressf("    %s %s\n", verbose.SuccessMark(), shimName)
	}
}

// createManifestBinShims creates shims for the manifest's bin entries.
func createManifestBinShims(c *shimCreator, installDir, shimDir string, m *manifest.Manifest) {
	bins := m.Binaries()
	if len(bins) == 0 {
		return
	}
	verbose.Progressf("  Creating shims...\n")
	for _, binPattern := range bins {
		binName, binAlias, extraArgs := shim.ParseBinPatternParts(binPattern)
		if binName == "" {
			continue
		}
		relBin := binRelPath(installDir, binName)
		c.tryCreate(shimNameForBin(binName, binAlias), filepath.Join(shimDir, relBin), binName, "", extraArgs)
	}
}

// binRelPath resolves a manifest bin entry to its path relative to the install
// directory: the resolved on-disk location when found, the Scoop "_" mirror when
// only that exists, and the raw entry otherwise.
func binRelPath(installDir, binName string) string {
	if _, rel, ok := resolveInstalledBinPath(installDir, binName, ""); ok {
		return rel
	}
	if _, err := os.Stat(filepath.Join(installDir, binName)); os.IsNotExist(err) {
		if _, statErr := os.Stat(filepath.Join(installDir, "_"+binName)); statErr == nil {
			return "_" + binName
		}
	}
	return binName
}

// createEnvAddPathShims creates a shim for every executable in the manifest's
// env_add_path directories.
func createEnvAddPathShims(c *shimCreator, installDir, shimDir string, m *manifest.Manifest) {
	envPaths := m.EnvAddPaths()
	if len(envPaths) == 0 {
		return
	}
	if len(m.Binaries()) == 0 {
		verbose.Progressf("  Creating shims (from env_add_path)...\n")
	}
	for _, dirPath := range envPaths {
		scanDir := installDir
		if dirPath != "." {
			scanDir = filepath.Join(installDir, dirPath)
		}
		entries, err := os.ReadDir(scanDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			verbose.Progressf("    cannot read directory %s: %v\n", scanDir, err)
			c.recordErr(fmt.Errorf("read %s: %w", scanDir, err))
			continue
		}
		createShimsFromDirEntries(c, entries, scanDir, dirPath, installDir, shimDir)
	}
}

// createShimsFromDirEntries creates a shim for each .exe/.cmd file of one scan pass.
func createShimsFromDirEntries(c *shimCreator, entries []os.DirEntry, scanDir, dirPath, installDir, shimDir string) {
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".exe") && !strings.HasSuffix(lower, ".cmd") {
			continue
		}
		exePath := filepath.Join(scanDir, name)
		rel, err := filepath.Rel(installDir, exePath)
		if err != nil {
			rel = name
		}
		c.tryCreate(envAddPathShimName(name), filepath.Join(shimDir, rel), name, dirPath, nil)
	}
}

// envAddPathShimName strips the executable extension from a scanned file name,
// covering lower- and upper-case spellings (possibly mixed, like App.EXE).
func envAddPathShimName(name string) string {
	shimName := strings.TrimSuffix(strings.TrimSuffix(name, ".exe"), ".cmd")
	return strings.TrimSuffix(strings.TrimSuffix(shimName, ".EXE"), ".CMD")
}

func shimNameForBin(exePath, alias string) string {
	if name := normalizeShimAlias(alias); name != "" {
		return name
	}
	name := filepath.Base(exePath)
	for _, ext := range []string{".exe", ".bat", ".cmd", ".sh", ".EXE", ".BAT", ".CMD", ".SH"} {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

func normalizeShimAlias(alias string) string {
	name := strings.TrimSpace(alias)
	if name == "" {
		return ""
	}
	for _, ext := range []string{".exe", ".bat", ".cmd", ".sh", ".EXE", ".BAT", ".CMD", ".SH"} {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

// GetShimsForPackage lists shim names whose prefix matches pkgName for the given version.
func GetShimsForPackage(e *runtime.Services, pkgName, version string) ([]string, error) {
	shims, err := e.ShimMgr.List()
	if err != nil {
		return nil, err
	}
	var result []string
	for _, s := range shims {
		if strings.HasPrefix(s.Name, pkgName) {
			result = append(result, s.Name)
		}
	}
	return result, nil
}
