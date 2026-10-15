package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gluestick-sh/core/apps"
	"github.com/gluestick-sh/core/cache"
	"github.com/gluestick-sh/core/engine/internal/install"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/shim"
)

// ListOptions contains options for listing packages
type ListOptions struct {
	Detailed bool
	ShowAll  bool
	Filter   []string
}

// activeListedInstall returns install dir/version only for completed installs.
func activeListedInstall(root, pkgName string, cacheIdx *cache.Index) (installDir, version string, ok bool) {
	pkgRoot := apps.PkgRoot(root, pkgName)

	if inst, registered := cacheIdx.GetInstalled(pkgName); registered {
		ver := inst.Version
		if versionDirInstalled(root, pkgName, ver) {
			dir := inst.InstallDir
			if dir == "" {
				dir = filepath.Join(pkgRoot, ver)
			}
			return dir, ver, true
		}
	}

	if ver, err := apps.ReadCurrent(pkgRoot); err == nil && ver != "" {
		if versionDirInstalled(root, pkgName, ver) {
			dir := filepath.Join(pkgRoot, ver)
			if _, err := apps.LoadInstallRecord(dir); err == nil {
				return dir, ver, true
			}
		}
	}

	versions, err := apps.ListVersions(pkgRoot)
	if err != nil {
		return "", "", false
	}
	for i := len(versions) - 1; i >= 0; i-- {
		ver := versions[i]
		dir := filepath.Join(pkgRoot, ver)
		if _, err := apps.LoadInstallRecord(dir); err == nil {
			return dir, ver, true
		}
	}
	return "", "", false
}

// listInstalledPackages lists packages with a completed install under apps/.
func (e *Engine) listInstalledPackages(ctx context.Context,
	opts ListOptions, reporter ProgressReporter,
) ([]*Package, error) {
	appsDir := filepath.Join(e.Config.RootDir, "apps")
	pkgEntries, err := os.ReadDir(appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read apps dir: %w", err)
	}

	cacheEntries := e.Cache.ListPackages()
	filterSet := newPackageFilterSet(opts.Filter)

	var packages []*Package
	for _, dirEntry := range pkgEntries {
		if !dirEntry.IsDir() {
			continue
		}
		name := dirEntry.Name()
		if !filterSet.matches(name) {
			continue
		}

		_, diskVersion, ok := activeListedInstall(e.Config.RootDir, name, e.Cache)
		if !ok {
			continue
		}

		packages = append(packages, e.buildListedPackage(name, diskVersion, cacheEntries[name], opts.Detailed))
	}

	// Sort by name
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})

	return packages, nil
}

// packageFilterSet matches package names against an optional filter list.
type packageFilterSet map[string]bool

// newPackageFilterSet builds a lookup for the filter names; an empty filter is nil.
func newPackageFilterSet(filter []string) packageFilterSet {
	if len(filter) == 0 {
		return nil
	}
	set := make(packageFilterSet, len(filter))
	for _, f := range filter {
		set[f] = true
	}
	return set
}

// matches reports whether name passes the filter; an empty set allows everything.
func (s packageFilterSet) matches(name string) bool {
	return len(s) == 0 || s[name]
}

// buildListedPackage assembles one listed package from disk and index data.
func (e *Engine) buildListedPackage(name, diskVersion string, entry *cache.PackageEntry, detailed bool) *Package {
	version := diskVersion
	var installedSize int64
	var installedAt string
	if entry != nil {
		version = e.resolveInstalledVersion(name, entry.Version)
		installedSize = entry.Size
		installedAt = entry.Installed
	}

	pkg := &Package{
		Name:          name,
		Version:       version,
		InstalledSize: installedSize,
		InstalledAt:   installedAt,
	}

	applyInstalledMetadata(e, pkg, name, version)

	detected := e.detectPackageVersion(name, version)
	pkg.DetectedVersion = detected.DetectedVersion
	pkg.VersionDetectSrc = detected.Source
	pkg.ExternallyUpdated = detected.ExternallyUpdated

	if detailed {
		applyShimBinaries(e, pkg, name, version)
	}

	return pkg
}

// applyInstalledMetadata copies bucket and manifest metadata onto the package.
func applyInstalledMetadata(e *Engine, pkg *Package, name, version string) {
	bucket, manifestInfo := e.getInstalledPackageDetails(name, version)
	switch {
	case manifestInfo != nil:
		pkg.Manifest = manifestInfo
		pkg.Description = manifestInfo.Description
		pkg.Homepage = manifestInfo.Homepage
		pkg.Bucket = bucket
	case bucket != "":
		pkg.Bucket = bucket
	}
}

// applyShimBinaries appends the package's shim names to its manifest binaries.
func applyShimBinaries(e *Engine, pkg *Package, name, version string) {
	shims, err := install.GetShimsForPackage(e.Services, name, version)
	if err != nil {
		return
	}
	if pkg.Manifest == nil {
		pkg.Manifest = &ManifestInfo{}
	}
	for _, shim := range shims {
		pkg.Manifest.Binaries = append(pkg.Manifest.Binaries, BinaryInfo{
			Name: shim,
		})
	}
}

// extractPackageFromPath extracts package name and version from a shim target path.
func (e *Engine) extractPackageFromPath(path string) (string, string) {
	return ParseInstallFilePath(path)
}

// getInstalledPackageDetails resolves bucket and manifest metadata for an installed package.
func (e *Engine) getInstalledPackageDetails(name, version string) (bucket string, info *ManifestInfo) {
	pkgRoot := apps.PkgRoot(e.Config.RootDir, name)
	bucket, info = e.installRecordDetails(pkgRoot, activeInstallVersion(pkgRoot, version))
	if bucket != "" {
		return bucket, info
	}

	// No bucket from the install record: fall back to scanning the bucket repos.
	// info may still carry manifest metadata read from the record, so keep it.
	if bucketName, m, ok := findBucketManifest(e.Config.RootDir, name); ok {
		return bucketName, e.createManifestInfo(m)
	}
	return "", info
}

// activeInstallVersion returns version, or the current-link version when version is empty.
func activeInstallVersion(pkgRoot, version string) string {
	if version != "" {
		return version
	}
	if v, err := apps.ReadCurrent(pkgRoot); err == nil {
		return v
	}
	return ""
}

// installRecordDetails reads bucket/manifest metadata from the version's install record.
// A missing version or unreadable record yields empty results.
func (e *Engine) installRecordDetails(pkgRoot, ver string) (bucket string, info *ManifestInfo) {
	if ver == "" {
		return "", nil
	}
	rec, err := apps.LoadInstallRecord(filepath.Join(pkgRoot, ver))
	if err != nil {
		return "", nil
	}
	if rec.Bucket != "" {
		bucket = rec.Bucket
	}
	if rec.Manifest != nil {
		info = e.createManifestInfo(rec.Manifest)
	}
	return bucket, info
}

// findBucketManifest locates name in the bucket repos under root and parses its manifest.
func findBucketManifest(root, name string) (bucketName string, m *manifest.Manifest, ok bool) {
	bucketsDir := filepath.Join(root, "buckets")
	entries, err := os.ReadDir(bucketsDir)
	if err != nil {
		return "", nil, false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := entry.Name()
		manifestPath := filepath.Join(bucketsDir, candidate, "bucket", name+".json")
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}
		parsed, err := manifest.ParseFile(manifestPath)
		if err != nil {
			continue
		}
		return candidate, parsed, true
	}
	return "", nil, false
}

// groupByPackage groups shims by their package (legacy support)
func (e *Engine) groupByPackage(configs []shim.Config) map[string]*Package {
	packages := make(map[string]*Package)

	for _, cfg := range configs {
		// Extract package info from path
		pkgName, version := e.extractPackageFromPath(cfg.Path)

		if pkgName == "" {
			// Skip shims that don't follow the pattern
			continue
		}

		if _, exists := packages[pkgName]; !exists {
			packages[pkgName] = &Package{
				Name:    pkgName,
				Version: version,
			}
		}

		// Add shim to manifest binaries
		if packages[pkgName].Manifest == nil {
			packages[pkgName].Manifest = &ManifestInfo{}
		}
		packages[pkgName].Manifest.Binaries = append(packages[pkgName].Manifest.Binaries, BinaryInfo{
			Name: cfg.Name,
		})
	}

	return packages
}

// ListLegacy lists packages using the shim-based approach (legacy)
func (e *Engine) ListLegacy(ctx context.Context,
	opts ListOptions,
	reporter ProgressReporter,
) ([]*Package, error) {
	// Get shim configs
	configs, err := e.ShimMgr.List()
	if err != nil {
		return nil, fmt.Errorf("list shims: %w", err)
	}

	// Group by package
	packages := e.groupByPackage(configs)

	// Apply filter if provided
	if len(opts.Filter) > 0 {
		filtered := make(map[string]*Package)
		filterSet := make(map[string]bool)
		for _, f := range opts.Filter {
			filterSet[f] = true
		}

		for name, pkg := range packages {
			if filterSet[name] {
				filtered[name] = pkg
			}
		}
		packages = filtered
	}

	// Convert to slice and sort
	var result []*Package
	for _, pkg := range packages {
		result = append(result, pkg)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// GetPackageStats returns statistics about installed packages
func (e *Engine) GetPackageStats() (*PackageStats, error) {
	entries := e.Cache.ListPackages()

	stats := &PackageStats{
		TotalPackages: int64(len(entries)),
		TotalSize:     0,
	}

	for _, entry := range entries {
		stats.TotalSize += entry.Size
	}

	return stats, nil
}

// PackageStats contains statistics about installed packages
type PackageStats struct {
	TotalPackages int64
	TotalSize     int64
}
