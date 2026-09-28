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
	shimEnv := ShimEnvForPackage(m, installDir, "")
	var firstErr error
	recordErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	tryCreateShim := func(shimName, shimTarget, label, fromDir string, extraArgs []string) {
		if owner, conflict := shim.OwnedByOtherPackage(shimsMetaDir, shimName, pkgName); conflict {
			verbose.Progressf("    skipped shim %s (already provided by %s)\n", shimName, owner)
			return
		}
		opts := shim.CreateOpts{Args: extraArgs, Env: shimEnv}
		if err := shimMgr.Create(shimName, shimTarget, opts); err != nil {
			verbose.Progressf("    %s failed to create shim for %s: %v\n", verbose.FailedMark(), label, err)
			recordErr(fmt.Errorf("shim %s: %w", shimName, err))
			return
		}
		if fromDir != "" {
			verbose.Progressf("    %s %s (from %s)\n", verbose.SuccessMark(), shimName, fromDir)
		} else {
			verbose.Progressf("    %s %s\n", verbose.SuccessMark(), shimName)
		}
	}

	if bins := m.Binaries(); len(bins) > 0 {
		verbose.Progressf("  Creating shims...\n")
		for _, binPattern := range bins {
			binName, binAlias, extraArgs := shim.ParseBinPatternParts(binPattern)
			if binName == "" {
				continue
			}
			shimName := shimNameForBin(binName, binAlias)
			relBin := binName
			exePath := filepath.Join(installDir, binName)
			if resolved, rel, ok := resolveInstalledBinPath(installDir, binName, ""); ok {
				exePath = resolved
				relBin = rel
			} else if _, err := os.Stat(exePath); os.IsNotExist(err) {
				altPath := filepath.Join(installDir, "_"+binName)
				if _, statErr := os.Stat(altPath); statErr == nil {
					exePath = altPath
					relBin = "_" + binName
				}
			}
			shimTarget := filepath.Join(shimDir, relBin)
			_ = exePath
			tryCreateShim(shimName, shimTarget, binName, "", extraArgs)
		}
	}

	if envPaths := m.EnvAddPaths(); len(envPaths) > 0 {
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
				recordErr(fmt.Errorf("read %s: %w", scanDir, err))
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				lower := strings.ToLower(name)
				if !strings.HasSuffix(lower, ".exe") && !strings.HasSuffix(lower, ".cmd") {
					continue
				}
				shimName := strings.TrimSuffix(strings.TrimSuffix(name, ".exe"), ".cmd")
				shimName = strings.TrimSuffix(strings.TrimSuffix(shimName, ".EXE"), ".CMD")
				exePath := filepath.Join(scanDir, name)
				rel, err := filepath.Rel(installDir, exePath)
				if err != nil {
					rel = name
				}
				shimTarget := filepath.Join(shimDir, rel)
				tryCreateShim(shimName, shimTarget, name, dirPath, nil)
			}
		}
	}

	return firstErr
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
