package install

import (
	"context"
	"fmt"
	"github.com/gluestick-sh/core/engine/internal/lifecycle"
	"github.com/gluestick-sh/core/shim"
	"path/filepath"

	"github.com/gluestick-sh/core/apps"
	"github.com/gluestick-sh/core/cache"
	"github.com/gluestick-sh/core/engine/internal/runtime"
	etypes "github.com/gluestick-sh/core/engine/types"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/message"
	"github.com/gluestick-sh/core/verbose"
)

// packageInCache checks if a package has a cache entry.
// This is used to determine if a package is already in the content cache,
// which allows skipping download on reinstall.
// Parameters:
//   - e: Runtime engine
//   - pkgName: Package name to check
//
// Returns true if the package exists in the cache.
func packageInCache(e *runtime.Services, pkgName string) bool {
	_, ok := e.Cache.Get(pkgName)
	return ok
}

// repairOrphanInstall completes installation when files exist but cache is missing.
// This handles the case where a package was partially installed (files on disk)
// but the cache index was not updated. It rescans the install directory and
// completes the registration (hooks, shims, cache index).
// Parameters:
//   - e: Runtime engine
//   - ctx: Context for cancellation
//   - pkgRef: Package reference string
//   - pkgName: Package name
//   - version: Package version
//   - m: Package manifest
//   - req: Installation request
//   - reporter: Progress reporter
//   - timings: Optional per-phase install timings
//
// Returns error if repair fails or install directory is invalid.
func repairOrphanInstall(e *runtime.Services,
	ctx context.Context,
	pkgRef, pkgName, version string,
	m *manifest.Manifest,
	req *etypes.InstallRequest,
	reporter etypes.ProgressReporter,
	timings *installPhaseTimings,
) error {
	root := e.Config.RootDir
	installDir := filepath.Join(apps.PkgRoot(root, pkgName), version)
	if !runtime.VersionDirInstalled(root, pkgName, version) {
		return fmt.Errorf("repair %s@%s: install directory missing", pkgName, version)
	}

	files, totalSize, err := cache.ScanInstallDir(installDir, e.Store)
	if err != nil {
		return fmt.Errorf("repair %s@%s: scan install dir: %w", pkgName, version, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("repair %s@%s: install directory has no indexed files", pkgName, version)
	}

	verbose.Progressf("  Completing registration for %s@%s (files on disk, missing cache index)...\n", pkgName, version)
	downloadName := lifecycle.DownloadNameFromManifest(m)
	return finalizePackageInstall(e,
		ctx, pkgRef, pkgName, m, installDir, downloadName, getFileExtensionFromURL(m.GetURL()),
		files, totalSize, req, reporter, timings,
	)
}

// finalizePackageInstall completes the installation process.
// This is the final phase that:
// 1. Restores persisted data (if any)
// 2. Runs pre-install hooks (if any)
// 3. Runs installer scripts (if any)
// 4. Flattens directory structure (if needed)
// 5. Validates manifest binaries
// 6. Runs post-install hooks
// 7. Creates the "current" symlink
// 8. Saves install record
// 9. Creates shims for binaries
// 10. Creates shortcuts (if any)
// 11. Updates cache index
//
// Parameters:
//   - e: Runtime engine
//   - ctx: Context for cancellation
//   - pkgRef: Package reference string
//   - pkgName: Package name
//   - m: Package manifest
//   - installDir: Directory where package is installed
//   - downloadName: Original download filename
//   - fileExt: File extension of the downloaded file
//   - installedFiles: Map of content hash -> relative path for installed files
//   - totalSize: Total size of all installed files
//   - req: Installation request
//   - reporter: Progress reporter
//   - timings: Optional per-phase install timings
//
// Returns error if any finalization step fails.
func finalizePackageInstall(e *runtime.Services,
	ctx context.Context,
	pkgRef, pkgName string,
	m *manifest.Manifest,
	installDir, downloadName, fileExt string,
	installedFiles map[string]string,
	totalSize int64,
	req *etypes.InstallRequest,
	reporter etypes.ProgressReporter,
	timings *installPhaseTimings,
) error {
	installArch := installArchitecture(req, m)
	s := &finalizeState{
		e:              e,
		ctx:            ctx,
		req:            req,
		reporter:       reporter,
		timings:        timings,
		pkgRef:         pkgRef,
		pkgName:        pkgName,
		m:              m,
		installDir:     installDir,
		downloadName:   downloadName,
		fileExt:        fileExt,
		root:           e.Config.RootDir,
		installArch:    installArch,
		persistDir:     filepath.Join(e.Config.RootDir, "persist", pkgName),
		persistEntries: m.PersistEntriesForInstall(installArch),
		installedFiles: installedFiles,
		totalSize:      totalSize,
	}

	if err := s.restorePersistData(); err != nil {
		return err
	}
	if err := s.runPreInstallHooks(); err != nil {
		return err
	}
	if err := s.runInstallerScript(); err != nil {
		return err
	}
	if err := s.flattenAndValidateBins(); err != nil {
		return err
	}
	if err := s.runPostInstallHooks(); err != nil {
		return err
	}
	if err := s.linkCurrentAndSaveRecord(); err != nil {
		return err
	}
	if err := s.createShimsAndShortcuts(); err != nil {
		return err
	}
	if err := s.updateCacheIndex(); err != nil {
		return err
	}

	verbose.Progressf("  %s %s@%s installed\n", verbose.SuccessMark(), pkgName, m.Version)
	s.report(PhaseComplete, StatusSuccess, 100, message.ProgressPackageInstallComplete, map[string]any{
		"package": pkgName,
	}, 0, 0)
	return nil
}

// finalizeState carries one package's finalize pass through its phases: the
// install inputs plus the working file index, which the phases that mutate the
// install directory refresh in place.
type finalizeState struct {
	e            *runtime.Services
	ctx          context.Context
	req          *etypes.InstallRequest
	reporter     etypes.ProgressReporter
	timings      *installPhaseTimings
	pkgRef       string
	pkgName      string
	m            *manifest.Manifest
	installDir   string
	downloadName string
	fileExt      string

	root           string
	installArch    string
	persistDir     string
	persistEntries []manifest.PersistEntry

	installedFiles map[string]string
	totalSize      int64
}

// report forwards a phase transition to the progress reporter.
func (s *finalizeState) report(phase etypes.Phase, status etypes.Status, pct float64, key string, args map[string]any, bytes, total int64) {
	runtime.ReportProgress(s.reporter, phase, s.pkgRef, status, pct, key, args, bytes, total)
}

// refreshInstalledFiles rescans the install directory into the working file index.
func (s *finalizeState) refreshInstalledFiles(step string) error {
	if err := refreshInstalledFilesFromDir(s.e.Store, s.installDir, s.installedFiles, &s.totalSize); err != nil {
		return fmt.Errorf("%s: %w", step, err)
	}
	return nil
}

// isScriptInstaller reports the .exe + installer.script layout, which manages its
// own files and therefore skips most finalize side effects.
func (s *finalizeState) isScriptInstaller() bool {
	return isExeInstallerScriptInstall(s.fileExt, s.m, s.installArch)
}

// restorePersistData restores persisted data for non script-installer packages.
func (s *finalizeState) restorePersistData() error {
	if len(s.persistEntries) == 0 || s.isScriptInstaller() {
		return nil
	}
	return lifecycle.RestorePersistOnInstall(s.installDir, s.persistDir, s.persistEntries)
}

// runPreInstallHooks applies pre_install handling: Scoop move-flatten manifests,
// the 7-Zip arm64 SFX layout repair, or the manifest's pre_install hook scripts.
func (s *finalizeState) runPreInstallHooks() error {
	if s.isScriptInstaller() {
		return nil
	}
	hooks := s.m.PreInstallHooksForInstall(s.installArch)
	if isScoopMoveFlattenPreInstall(hooks) {
		if err := ensureScoopMoveFlattenInstallDir(s.installDir, s.installedFiles); err != nil {
			return err
		}
		hooks = nil
	}
	if isSevenZipArm64SFXPreInstall(s.pkgName, s.installArch, hooks) {
		return applySevenZipArm64SFXInstall(s.e, s.ctx, s.installDir, s.downloadName, archiveHashForDownload(s.installedFiles, s.downloadName), s.m)
	}
	if len(hooks) > 0 {
		return s.runHookScripts(hooks)
	}
	return nil
}

// runHookScripts resolves 7z/dark helpers, runs the pre_install hooks, and
// re-indexes whatever they changed on disk.
func (s *finalizeState) runHookScripts(hooks []string) error {
	if err := lifecycle.PrepareInstallDirForPreInstallHooks(s.installDir, s.persistDir, s.persistEntries); err != nil {
		return err
	}
	sevenZip, dark, err := lifecycle.ResolveHookHelpers(s.e, s.ctx, hooks, s.pkgName, s.timings.runBootstrap)
	if err != nil {
		return err
	}
	env := lifecycle.NewHookScriptEnv(s.e, s.installDir, s.downloadName, s.m.Version, s.pkgRef, s.pkgName, s.installArch, hooks)
	env.SevenZip = sevenZip
	env.Dark = dark
	if err := lifecycle.RunPreInstallHooks(env); err != nil {
		return err
	}
	return s.refreshInstalledFiles("index installed files after pre_install")
}

// runInstallerScript runs installer.script for non-.exe packages and re-indexes
// the files it changed.
func (s *finalizeState) runInstallerScript() error {
	if !s.m.HasInstallerScriptForInstall(s.installArch) || s.isScriptInstaller() {
		return nil
	}
	if err := runManifestInstallerHook(s.e, s.ctx, s.installDir, s.downloadName, s.pkgName, s.pkgRef, s.m, s.installArch, installInteractive(s.req)); err != nil {
		return err
	}
	return s.refreshInstalledFiles("index installed files after installer script")
}

// flattenAndValidateBins flattens extract_dir layouts and validates manifest bins.
func (s *finalizeState) flattenAndValidateBins() error {
	flattened, err := ensureExtractDirFlattened(s.installDir, s.m, s.installArch)
	if err != nil {
		return err
	}
	if flattened {
		if err := s.refreshInstalledFiles("index installed files after flatten"); err != nil {
			return err
		}
	}
	return validateManifestBins(s.installDir, s.m)
}

// runPostInstallHooks flattens single-file layouts, runs post_install, refreshes
// the file index when the hooks touch files, and repairs the 7-Zip arm64 layout.
func (s *finalizeState) runPostInstallHooks() error {
	if s.isScriptInstaller() {
		return nil
	}
	if err := s.moveFlattenIfNeeded(); err != nil {
		return err
	}
	if err := runManifestPostInstall(s.e, s.ctx, s.pkgRef, s.pkgName, s.installDir, s.downloadName, s.m.Version, s.installArch, s.m, s.timings); err != nil {
		return err
	}
	if postInstallNeedsFileIndexRefresh(s.m.PostInstallHooksForInstall(s.installArch)) {
		if err := s.refreshInstalledFiles("index installed files after post_install"); err != nil {
			return err
		}
	}
	if err := repairSevenZipArm64Layout(s.installDir); err != nil {
		return fmt.Errorf("repair 7zip arm64 layout: %w", err)
	}
	applyInstallContextRegistry(s.installDir)
	return nil
}

// moveFlattenIfNeeded flattens Scoop move-flatten layouts and bin-only directories.
func (s *finalizeState) moveFlattenIfNeeded() error {
	preHooks := s.m.PreInstallHooksForInstall(s.installArch)
	if !isScoopMoveFlattenPreInstall(preHooks) && !installNeedsMoveFlatten(s.installDir) {
		return nil
	}
	return ensureScoopMoveFlattenInstallDir(s.installDir, s.installedFiles)
}

// linkCurrentAndSaveRecord switches the package's current link to the installed
// version and writes its install record.
func (s *finalizeState) linkCurrentAndSaveRecord() error {
	s.report(PhaseLink, StatusRunning, 0, message.ProgressLinkingFiles, nil, 0, 0)
	if err := apps.LinkCurrent(apps.PkgRoot(s.root, s.pkgName), s.m.Version); err != nil {
		return fmt.Errorf("link current: %w", err)
	}
	verbose.Progressf("  Linking current => %s\n", s.m.Version)

	if err := apps.SaveInstallRecord(s.installDir, runtime.PackageBucketName(s.pkgRef), s.m); err != nil {
		return fmt.Errorf("save install record: %w", err)
	}
	return nil
}

// createShimsAndShortcuts replaces the package's shims, applies env_set, and
// creates start-menu shortcuts.
func (s *finalizeState) createShimsAndShortcuts() error {
	s.report(PhaseShim, StatusRunning, 0, message.ProgressCreatingShims, nil, 0, 0)

	pkgRoot := apps.PkgRoot(s.root, s.pkgName)
	appsDir := filepath.Join(s.root, "apps")
	currentDir := filepath.Join(pkgRoot, apps.CurrentLinkName)
	shimsMetaDir := filepath.Join(s.root, "shims-meta")
	if _, err := shim.RemoveShimsForPackage(s.e.ShimMgr, shimsMetaDir, appsDir, s.pkgName); err != nil {
		return fmt.Errorf("remove old shims: %w", err)
	}

	if err := s.runShimTimed(func() error {
		return createPackageShims(s.e.ShimMgr, shimsMetaDir, s.pkgName, s.installDir, currentDir, s.m)
	}); err != nil {
		return fmt.Errorf("create shims: %w", err)
	}

	if err := ApplyPackageEnvSet(s.m, s.installDir, s.installArch); err != nil {
		return fmt.Errorf("apply env_set: %w", err)
	}

	if err := lifecycle.CreatePackageShortcuts(s.installDir, s.m, s.installArch); err != nil {
		return fmt.Errorf("create shortcuts: %w", err)
	}
	return nil
}

// updateCacheIndex records the installed files and install location in the cache.
func (s *finalizeState) updateCacheIndex() error {
	s.report(PhaseIndex, StatusRunning, 0, message.ProgressUpdatingCache, nil, 0, 0)

	if err := s.runCacheTimed(func() error {
		if err := s.e.Cache.Add(s.pkgName, s.m.Version, s.installedFiles, s.totalSize); err != nil {
			return err
		}
		metadata := preserveInstalledMetadata(s.e.Cache, s.pkgName)
		return s.e.Cache.AddInstalled(s.pkgName, s.m.Version, s.installDir, s.totalSize, metadata)
	}); err != nil {
		return fmt.Errorf("update cache index: %w", err)
	}
	return nil
}

// runShimTimed and runCacheTimed time the shim and cache phases when install
// timings are enabled.
func (s *finalizeState) runShimTimed(fn func() error) error {
	if s.timings == nil {
		return fn()
	}
	return s.timings.runShim(fn)
}

func (s *finalizeState) runCacheTimed(fn func() error) error {
	if s.timings == nil {
		return fn()
	}
	return s.timings.runCache(fn)
}
