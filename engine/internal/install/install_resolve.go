package install

import (
	"errors"
	"fmt"

	"github.com/gluestick-sh/core/engine/internal/catalog"
	"github.com/gluestick-sh/core/engine/internal/override"
	"github.com/gluestick-sh/core/engine/internal/runtime"
	etypes "github.com/gluestick-sh/core/engine/types"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/message"
	"github.com/gluestick-sh/core/verbose"
)

// resolveInstallPhase handles the first phase of installation.
// This phase prepares the installation by:
// 1. Ensuring the required bucket is available
// 2. Finding and parsing the package manifest
// 3. Handling version pinning (e.g., package@1.2.3)
// 4. Checking for existing installations and version locks
// 5. Applying manifest overrides (local customizations)
// 6. Determining the installation architecture (arm64, amd64, etc.)
//
// Parameters:
//   - state: Current installation state (updated with resolution results)
//
// Returns error if:
// - Bucket cannot be found or accessed
// - Manifest cannot be parsed
// - Version pinning is requested but unsupported
// - Package is version-locked at a different version
// - Installation should be skipped (already installed)
//
// Side effects:
// - Updates state.pkgName, state.targetVersion, state.manifest
// - May return nil error if package is already installed (success case)
func resolveInstallPhase(state *installState) error {
	// Parse package reference
	state.pkgName, state.pinVersion = runtime.ParsePkgRef(state.pkgRef)
	state.resolveOverrideRef()

	// Phase 1.1: Ensure bucket + find manifest
	state.report(etypes.PhaseResolve, etypes.StatusRunning, 0, message.ProgressResolvingManifest, nil, 0, 0)
	m, err := state.loadManifest()
	if err != nil {
		return err
	}

	// Phase 1.2-1.3: version pinning, then existing-install fast paths
	if m, err = state.resolvePinnedVersion(m); err != nil {
		return err
	}
	state.setTargetVersion(m)
	if err := state.checkExistingInstall(m); err != nil {
		return err
	}
	if state.done {
		return nil
	}

	// Phase 1.4-1.6: display the plan, then apply manifest overrides
	state.describePlan(m)
	if m, err = state.applyManifestOverrides(m); err != nil {
		return err
	}
	state.manifest = m
	return nil
}

// report forwards a phase update to the install reporter.
func (s *installState) report(phase etypes.Phase, status etypes.Status, pct float64, key string, args map[string]any, bytes, total int64) {
	runtime.ReportProgress(s.reporter, phase, s.pkgRef, status, pct, key, args, bytes, total)
}

// resolveOverrideRef records the catalog override ref when one is resolvable.
func (s *installState) resolveOverrideRef() {
	if resolved, err := catalog.ResolveInstallRef(s.engine, s.ctx, s.pkgRef); err == nil && resolved != "" {
		s.overrideRef = resolved
	}
}

// loadManifest ensures the bucket exists and returns the package manifest,
// recording its path on the state.
func (s *installState) loadManifest() (*manifest.Manifest, error) {
	if err := catalog.EnsureBucketForInstall(s.engine, s.ctx, s.lookupRef, s.req.Buckets); err != nil {
		return nil, err
	}
	manifestPath, m, err := s.engine.BucketRegistry.GetManifestPath(s.lookupRef)
	if err != nil {
		return nil, catalog.WrapManifestNotFound(s.engine, s.pkgRef, err)
	}
	s.manifestPath = manifestPath
	return m, nil
}

// resolvePinnedVersion generates the manifest for a package@version pin when the
// bucket ships a different version.
func (s *installState) resolvePinnedVersion(m *manifest.Manifest) (*manifest.Manifest, error) {
	if s.pinVersion == "" || m.Version == s.pinVersion {
		return m, nil
	}
	verbose.Progressf("  Generating manifest for %s@%s (bucket has %s)...\n", s.pkgName, s.pinVersion, m.Version)
	generated, err := m.ForVersion(s.pkgName, s.pinVersion)
	if err != nil {
		if errors.Is(err, manifest.ErrNoAutoupdate) {
			return nil, fmt.Errorf("%s: use a manifest with autoupdate or install without @version", err)
		}
		return nil, err
	}
	return generated, nil
}

// setTargetVersion records the version to install: the pinned @version when
// present, otherwise the manifest version.
func (s *installState) setTargetVersion(m *manifest.Manifest) {
	s.targetVersion = m.Version
	if s.pinVersion != "" {
		s.targetVersion = s.pinVersion
	}
}

// checkExistingInstall handles the already-installed fast paths. It marks the
// state done (complete without fetch/deploy) or flags the previous directory for
// a clean reinstall.
func (s *installState) checkExistingInstall(m *manifest.Manifest) error {
	if s.req.Force {
		return nil
	}
	if err := s.guardVersionLock(); err != nil {
		return err
	}
	if !runtime.VersionDirInstalled(s.engine.Config.RootDir, s.pkgName, s.targetVersion) {
		return nil
	}
	return s.reuseOrActivateVersion(m)
}

// guardVersionLock refuses to move a version-locked package off its pinned
// version when installing without an explicit @version.
func (s *installState) guardVersionLock() error {
	if s.pinVersion != "" {
		return nil
	}
	installedVersion, ok := runtime.InstalledPackage(s.engine.Config.RootDir, s.pkgName)
	if !ok {
		return nil
	}
	if IsPackageVersionLocked(s.engine, s.pkgName) && installedVersion != s.targetVersion {
		return fmt.Errorf("%s is version-locked at %s; remove the lock or install with @version", s.pkgName, installedVersion)
	}
	return nil
}

// reuseOrActivateVersion completes an install whose target version directory
// already exists: as the active version, or as a ready-but-inactive local one.
func (s *installState) reuseOrActivateVersion(m *manifest.Manifest) error {
	root := s.engine.Config.RootDir
	if ActiveInstallVersion(root, s.pkgName) == s.targetVersion {
		return s.reuseActiveVersion(m)
	}
	return s.activateLocalVersionOrReinstall(m)
}

// reuseActiveVersion completes the already-active target version: repairs a
// broken current link, reports it as installed, or finishes an orphaned
// registration; otherwise flags the directory for a clean reinstall.
func (s *installState) reuseActiveVersion(m *manifest.Manifest) error {
	root := s.engine.Config.RootDir
	if ver, needs := packageNeedsCurrentRepair(root, s.pkgName); needs && ver == s.targetVersion {
		if err := repairBrokenCurrentInstall(s.engine, s.pkgRef, s.pkgName, ver, m); err != nil {
			return err
		}
		s.done = true
		return nil
	}
	if packageInCache(s.engine, s.pkgName) {
		verbose.Progressf("  %s\n", runtime.FormatAlreadyInstalled(s.pkgName, s.targetVersion))
		s.done = true
		return nil
	}
	if orphanInstallRepairable(root, s.pkgName, s.targetVersion, m) {
		if err := repairOrphanInstall(s.engine, s.ctx, s.pkgRef, s.pkgName, s.targetVersion, m, s.req, s.reporter, s.timings); err != nil {
			return err
		}
		s.done = true
		return nil
	}
	verbose.Progressf("  Previous install incomplete; reinstalling...\n")
	s.clearInstallDir = true
	return nil
}

// activateLocalVersionOrReinstall activates a ready-but-inactive local version,
// or flags the incomplete directory for a clean reinstall.
func (s *installState) activateLocalVersionOrReinstall(m *manifest.Manifest) error {
	if !LocalVersionReadyToActivate(s.engine.Config.RootDir, s.pkgName, s.targetVersion, m) {
		verbose.Progressf("  Previous install incomplete; reinstalling...\n")
		s.clearInstallDir = true
		return nil
	}
	if err := activateLocalVersion(s.engine, s.ctx, s.pkgRef, s.pkgName, s.targetVersion, m, s.req, s.reporter, s.timings); err != nil {
		return err
	}
	s.done = true
	return nil
}

// describePlan prints the resolved description, version, and architecture.
func (s *installState) describePlan(m *manifest.Manifest) {
	if m.Description != "" {
		verbose.Progressf("  %s\n", m.Description)
	}
	verbose.Progressf("  Version: %s\n", m.Version)

	s.installArch = installArchitecture(s.req, m)
	if s.installArch != "" {
		verbose.Progressf("  Architecture: %s\n", s.installArch)
	}
}

// applyManifestOverrides applies local manifest overrides and guards against the
// override layer dropping the manifest.
func (s *installState) applyManifestOverrides(m *manifest.Manifest) (*manifest.Manifest, error) {
	overridden, err := override.ApplyManifestOverrides(s.engine, s.overrideRef, s.manifestPath, m, s.installArch, s.req)
	if err != nil {
		return nil, err
	}
	if overridden == nil {
		return nil, fmt.Errorf("manifest is nil after overrides for %s (internal error)", s.pkgRef)
	}
	return overridden, nil
}
