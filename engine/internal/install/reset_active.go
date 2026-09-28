package install

import (
	"github.com/gluestick-sh/core/shim"
	"github.com/gluestick-sh/core/engine/internal/lifecycle"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gluestick-sh/core/apps"
	"github.com/gluestick-sh/core/engine/internal/runtime"
)

// ResetPackage relinks current, shims, and shortcuts for an installed version.
func ResetPackage(e *runtime.Services, appsDir, shimsMetaDir, pkgRef string) error {
	pkgName, targetVer := runtime.ParsePkgRef(pkgRef)
	pkgRoot := apps.PkgRoot(e.Config.RootDir, pkgName)

	if targetVer == "" {
		var err error
		targetVer, err = apps.ReadCurrent(pkgRoot)
		if err != nil || targetVer == "" {
			targetVer, err = apps.EnsureCurrent(pkgRoot)
			if err != nil || targetVer == "" {
				return fmt.Errorf("%s", runtime.FormatNotInstalled(pkgName))
			}
		}
	}

	installDir := filepath.Join(pkgRoot, targetVer)
	if st, err := os.Stat(installDir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s@%s is not installed", pkgName, targetVer)
	}

	if err := apps.LinkCurrent(pkgRoot, targetVer); err != nil {
		return fmt.Errorf("link current: %w", err)
	}

	m, err := lifecycle.LoadManifestForReset(e, installDir, pkgName)
	if err != nil {
		return err
	}

	if _, err := shim.RemoveShimsForPackage(e.ShimMgr, shimsMetaDir, appsDir, pkgName); err != nil {
		return fmt.Errorf("remove shims: %w", err)
	}

	currentDir := filepath.Join(pkgRoot, apps.CurrentLinkName)
	if err := createPackageShims(e.ShimMgr, shimsMetaDir, pkgName, installDir, currentDir, m); err != nil {
		return fmt.Errorf("create shims: %w", err)
	}

	if err := RefreshPackageShellIntegration(installDir, m, m.SelectedArchitecture()); err != nil {
		return fmt.Errorf("refresh shortcuts: %w", err)
	}

	if _, ok := e.Cache.Get(pkgName); ok {
		if err := e.Cache.SetActiveVersionInstallDir(pkgName, targetVer, installDir); err != nil {
			return fmt.Errorf("update cache index: %w", err)
		}
	}
	return nil
}

