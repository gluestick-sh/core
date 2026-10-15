// Package uninstall removes installed package versions, running manifest
// uninstall hooks and cleaning up shortcuts, shims, links, and cache entries.
package uninstall

import (
	"context"
	"fmt"
	"github.com/gluestick-sh/core/shim"
	"os"
	"path/filepath"

	"github.com/gluestick-sh/core/apps"
	"github.com/gluestick-sh/core/cache"
	"github.com/gluestick-sh/core/engine/internal/lifecycle"
	"github.com/gluestick-sh/core/engine/internal/runtime"
	etypes "github.com/gluestick-sh/core/engine/types"
	"github.com/gluestick-sh/core/humanize"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/procutil"
	"github.com/gluestick-sh/core/store"
	"github.com/gluestick-sh/core/verbose"
)

// PackageFull implements the complete uninstallation logic.
// This function handles all aspects of package removal including:
// - Running uninstall hooks
// - Removing shortcuts
// - Saving persist data
// - Removing version directory
// - Cleaning up shims
// - Updating cache index
// - Switching to remaining versions (if any)
// Parameters:
//   - e: Runtime engine
//   - ctx: Context for cancellation
//   - pkgRef: Package reference (e.g., "package" or "package@version")
//   - req: Uninstallation request with options
//
// Returns:
//   - The version that was uninstalled
//   - Error if uninstallation fails
func PackageFull(e *runtime.Services,
	ctx context.Context,
	pkgRef string,
	req *etypes.UninstallRequest,
) (removedVer string, activateVer string, err error) {
	if err := runtime.ContextCanceled(ctx); err != nil {
		return "", "", err
	}
	root := e.Config.RootDir
	appsDir := filepath.Join(root, "apps")
	shimsMetaDir := filepath.Join(root, "shims-meta")

	pkgName, targetVer := runtime.ParsePkgRef(pkgRef)
	pkgRoot := apps.PkgRoot(root, pkgName)

	versions, err := apps.ListVersions(pkgRoot)
	if err != nil {
		return "", "", err
	}
	if len(versions) == 0 {
		version, handled, err := cleanupLeftoverInstall(e, appsDir, shimsMetaDir, pkgName, pkgRoot, req.Purge)
		if err != nil {
			return "", "", err
		}
		if handled {
			return version, "", nil
		}
		return "", "", fmt.Errorf("%s", runtime.FormatNotInstalled(pkgName))
	}

	targetVer = resolveUninstallVersion(pkgRoot, targetVer)
	if !runtime.VersionDirInstalled(root, pkgName, targetVer) {
		return "", "", fmt.Errorf("%s@%s is not installed", pkgName, targetVer)
	}

	currentVer, _ := apps.ReadCurrent(pkgRoot)
	verbose.Progressf("Uninstalling %s@%s...\n", pkgName, targetVer)
	j := &uninstallJob{
		e:            e,
		ctx:          ctx,
		req:          req,
		root:         root,
		appsDir:      appsDir,
		shimsMetaDir: shimsMetaDir,
		pkgName:      pkgName,
		targetVer:    targetVer,
		pkgRoot:      pkgRoot,
		verDir:       filepath.Join(pkgRoot, targetVer),
		wasActive:    currentVer == targetVer,
	}
	j.reportRemovalSize()

	if err := j.runUninstallHooksAndSavePersist(); err != nil {
		return "", "", err
	}
	if err := j.removeCurrentAndVersionDir(); err != nil {
		return "", "", err
	}
	newActive, err := j.finishAfterRemoval()
	if err != nil {
		return "", "", err
	}
	return j.targetVer, newActive, nil
}

// uninstallJob carries one full-uninstall pass through its phases.
type uninstallJob struct {
	e   *runtime.Services
	ctx context.Context
	req *etypes.UninstallRequest

	root         string
	appsDir      string
	shimsMetaDir string

	pkgName   string
	targetVer string
	pkgRoot   string
	verDir    string
	wasActive bool

	manifest    *manifest.Manifest
	bucketName  string
	installArch string
}

// cleanupLeftoverInstall finishes a package that has no version directories on
// disk but leftover cache/shim metadata. handled reports whether such leftovers
// were found and removed; version is the cache-recorded version when known.
func cleanupLeftoverInstall(e *runtime.Services, appsDir, shimsMetaDir, pkgName, pkgRoot string, purge bool) (version string, handled bool, err error) {
	entry, cacheOk := e.Cache.Get(pkgName)
	if !cacheOk && !shim.HasShimsForPackage(shimsMetaDir, pkgName) {
		return "", false, nil
	}
	if cacheOk {
		version = entry.Version
	}
	verbose.Progressf("Cleaning up %s (no install files on disk)...\n", pkgName)
	if err := finishPackageRemoved(e, appsDir, shimsMetaDir, pkgName, pkgRoot, version, purge); err != nil {
		return "", false, err
	}
	return version, true, nil
}

// resolveUninstallVersion picks the version to remove: the pinned @version, the
// active version, or any remaining version.
func resolveUninstallVersion(pkgRoot, requested string) string {
	if requested != "" {
		return requested
	}
	if v, _ := apps.ReadCurrent(pkgRoot); v != "" {
		return v
	}
	if v, _ := apps.EnsureCurrent(pkgRoot); v != "" {
		return v
	}
	v, _ := apps.PickDefaultVersion(pkgRoot)
	return v
}

// reportRemovalSize prints what is about to be removed from the version directory.
func (j *uninstallJob) reportRemovalSize() {
	fileCount, dirSize := CountInstallDir(j.verDir)
	verbose.Progressf("  Removing %s", j.verDir)
	if fileCount > 0 {
		verbose.Progressf(" (%d file(s), %s)", fileCount, humanize.FormatBytes(dirSize))
	}
	verbose.Progressf("\n")
}

// runUninstallHooksAndSavePersist runs pre_uninstall hooks, removes shortcuts,
// releases file locks, blocks on running processes, and saves persist data.
func (j *uninstallJob) runUninstallHooksAndSavePersist() error {
	j.manifest, j.bucketName, j.installArch = loadUninstallManifestContext(j.e, j.verDir, j.pkgName)
	if j.manifest != nil {
		if err := runManifestUninstallHooks(j.e, j.ctx, j.bucketName, j.pkgName, j.verDir, j.targetVer, j.installArch, j.manifest); err != nil {
			return err
		}
		if err := lifecycle.RemovePackageShortcuts(j.manifest, j.installArch); err != nil {
			verbose.Progressf("  Warning: failed to remove shortcuts: %v\n", err)
		}
	}

	if err := runtime.ContextCanceled(j.ctx); err != nil {
		return err
	}
	releaseInstallDirFileLocks(j.verDir)

	if err := checkProcessesBlockingUninstall(j.pkgRoot, j.verDir, j.targetVer, j.pkgName); err != nil {
		return err
	}
	return j.savePersistData()
}

// savePersistData copies persist entries out of the version directory before it
// is removed.
func (j *uninstallJob) savePersistData() error {
	if j.manifest == nil {
		return nil
	}
	persistEntries := j.manifest.PersistEntriesForInstall(j.installArch)
	if len(persistEntries) == 0 {
		return nil
	}
	persistDir := filepath.Join(j.root, "persist", j.pkgName)
	if err := lifecycle.SavePersistOnUninstall(j.verDir, persistDir, persistEntries); err != nil {
		return fmt.Errorf("save persist data: %w", err)
	}
	return nil
}

// removeCurrentAndVersionDir removes the active version link (when the target was
// active) and then the version directory, restoring the link if the removal
// fails.
func (j *uninstallJob) removeCurrentAndVersionDir() error {
	removedCurrent := false
	if j.wasActive {
		if err := apps.RemoveCurrent(j.pkgRoot); err != nil {
			return fmt.Errorf("remove current link: %w", err)
		}
		removedCurrent = true
	}

	if err := runtime.ContextCanceled(j.ctx); err != nil {
		return err
	}
	if err := lifecycle.RemoveAll(j.verDir); err != nil {
		return j.removeVersionDirError(err, removedCurrent)
	}
	return nil
}

// removeVersionDirError enriches a failed version-directory removal: it restores
// the current link, reports processes that may hold the directory open, and adds
// the access-denied hint where applicable.
func (j *uninstallJob) removeVersionDirError(err error, removedCurrent bool) error {
	if removedCurrent {
		if linkErr := apps.LinkCurrent(j.pkgRoot, j.targetVer); linkErr != nil {
			verbose.Progressf("  Warning: failed to restore current link after uninstall error: %v\n", linkErr)
		}
	}
	blockDirs := []string{j.verDir}
	if cur, readErr := apps.ReadCurrent(j.pkgRoot); readErr == nil && cur == j.targetVer {
		blockDirs = append(blockDirs, filepath.Join(j.pkgRoot, apps.CurrentLinkName))
	}
	if procs, _ := procutil.ProcessesBlockingUninstall(blockDirs...); len(procs) > 0 {
		return processesBlockingUninstallError(j.pkgName, j.targetVer, procs)
	}
	if isAccessDeniedRemoveErr(err) {
		return fmt.Errorf("remove version dir: %w\n%s", err, accessDeniedUninstallHint(j.pkgName))
	}
	return fmt.Errorf("remove version dir: %w", err)
}

// finishAfterRemoval runs post_uninstall hooks and completes the removal: when no
// versions remain it clears package metadata, otherwise it switches the active
// version when the removed one was active. It returns the version to activate
// ("" when none).
func (j *uninstallJob) finishAfterRemoval() (activateVer string, err error) {
	if j.manifest != nil {
		if err := runManifestPostUninstallHooks(j.e, j.ctx, j.bucketName, j.pkgName, j.verDir, j.targetVer, j.installArch, j.manifest); err != nil {
			verbose.Progressf("  Warning: post_uninstall failed: %v\n", err)
		}
	}

	remaining, err := apps.ListVersions(j.pkgRoot)
	if err != nil {
		return "", err
	}

	if len(remaining) == 0 {
		if err := j.removeLastVersion(); err != nil {
			return "", err
		}
		return "", nil
	}

	if j.wasActive {
		newVer, ok := apps.PickDefaultVersion(j.pkgRoot)
		if !ok {
			return "", fmt.Errorf("no version left to activate for %s", j.pkgName)
		}
		verbose.Progressf("  Switching current => %s\n", newVer)
		if err := apps.LinkCurrent(j.pkgRoot, newVer); err != nil {
			return "", fmt.Errorf("link current: %w", err)
		}
		// The caller (engine facade) relinks shims/shortcuts for the newly
		// active version via install.ResetPackage; uninstall itself never
		// reaches into install.
		return newVer, nil
	}

	verbose.Progressf("  %s is not the active version; shims unchanged\n", j.targetVer)

	if j.req.Purge {
		verbose.Progressf("  Cache kept (other versions installed; purge after removing all versions)\n")
	}
	verbose.Progressf("  %s %s@%s uninstalled\n", verbose.SuccessMark(), j.pkgName, j.targetVer)
	return "", nil
}

// removeLastVersion clears package-level metadata (env_set, shims, cache records)
// after the final version directory is gone.
func (j *uninstallJob) removeLastVersion() error {
	if j.manifest != nil {
		if err := lifecycle.RemovePackageEnvSet(j.manifest, j.installArch); err != nil {
			verbose.Progressf("  Warning: failed to remove env_set: %v\n", err)
		}
	}
	return finishPackageRemoved(j.e, j.appsDir, j.shimsMetaDir, j.pkgName, j.pkgRoot, j.targetVer, j.req.Purge)
}

// finishPackageRemoved performs final cleanup after the last version is removed.
// This handles shim removal, cache purging (if requested), and status updates.
// Parameters:
//   - e: Runtime engine
//   - appsDir: Applications directory
//   - shimsMetaDir: Shims metadata directory
//   - pkgName: Package name
//   - pkgRoot: Package root directory
//   - version: Version being removed
//   - purge: If true, remove cached blobs
//
// Returns error if cleanup fails.
func finishPackageRemoved(e *runtime.Services,
	appsDir, shimsMetaDir, pkgName, pkgRoot, version string,
	purge bool,
) error {
	removedShims, err := shim.RemoveShimsForPackage(e.ShimMgr, shimsMetaDir, appsDir, pkgName)
	if err != nil {
		verbose.Progressf("    Warning: failed to remove shims: %v\n", err)
	} else if len(removedShims) > 0 {
		verbose.Progressf("  Removing shims...\n")
		for _, shimName := range removedShims {
			verbose.Progressf("    %s %s\n", verbose.SuccessMark(), shimName)
		}
	}

	if err := lifecycle.RemoveAll(pkgRoot); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove install directory: %w", err)
	}

	if purge {
		entry, _ := e.Cache.Get(pkgName)
		if entry != nil {
			verbose.Progressf("  Purging cache index...\n")
			if err := purgePackageCache(e.Cache, e.Store, appsDir, pkgName, entry); err != nil {
				verbose.Progressf("    Warning: failed to purge cache: %v\n", err)
			}
		} else {
			verbose.Progressf("  No cache index entry to purge\n")
		}
		verbose.Progressf("  %s %s@%s uninstalled and cache purged\n", verbose.SuccessMark(), pkgName, version)
	} else {
		if _, ok := e.Cache.GetInstalled(pkgName); ok {
			if err := e.Cache.RemoveInstalled(pkgName); err != nil {
				return fmt.Errorf("update install registry: %w", err)
			}
		}
		if _, ok := e.Cache.Get(pkgName); ok {
			// verbose.Progressf("  Install removed; content cache kept for %s@%s (%s, %d files — reinstall skips download)\n",
			// 	pkgName, entry.Version, humanize.FormatBytes(entry.Size), len(entry.Files))
			verbose.Progressf("  Install removed\n")
			// verbose.Progressf("  Use uninstall --purge to delete cached blobs\n")
		} else {
			verbose.Progressf("  Install removed (no content cache entry)\n")
		}
		verbose.Progressf("  %s %s@%s uninstalled\n", verbose.SuccessMark(), pkgName, version)
	}
	return nil
}

// purgePackageCache removes cached blobs for a package.
// This deletes both the cache index entry and the CAS blobs,
// freeing disk space. This is only called when purging the last version.
// Parameters:
//   - cacheIdx: Cache index
//   - store: CAS store
//   - appsDir: Applications directory (for reference checking)
//   - pkgName: Package name
//   - entry: Cache entry for the package
//
// Returns error if cache removal fails.
func purgePackageCache(cacheIdx *cache.Index,
	store *store.Store,
	appsDir, pkgName string,
	entry *cache.PackageEntry,
) error {
	if entry != nil {
		verbose.Progressf("    Index entry: %d file(s), %s\n", len(entry.Files), humanize.FormatBytes(entry.Size))
	}
	removed, freed, err := cache.PurgePackage(cacheIdx, store, appsDir, pkgName)
	if err != nil {
		return err
	}
	verbose.Progressf("    Removed %d cached file(s), %s\n", removed, humanize.FormatBytes(freed))
	return nil
}

// CountInstallDir returns file count and total size for a package version directory.
// This walks the directory tree recursively to count files and sum their sizes.
// Used for reporting how much will be removed during uninstallation.
// Parameters:
//   - pkgDir: Package version directory to scan
//
// Returns:
//   - fileCount: Number of files (excluding directories)
//   - totalSize: Total size of all files in bytes
func CountInstallDir(pkgDir string) (fileCount int, totalSize int64) {
	return apps.CountInstallDir(pkgDir)
}
