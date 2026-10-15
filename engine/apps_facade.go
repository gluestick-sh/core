package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gluestick-sh/core/apps"
)

// Layout constants for ~/.glue/apps (re-exported for path construction).
const (
	AppsCurrentLinkName   = apps.CurrentLinkName
	AppsInstallRecordFile = apps.InstallRecordFile
)

// ParseInstallFilePath extracts package name and version from a path under apps/<pkg>/<version>/.
func ParseInstallFilePath(path string) (pkgName, version string) {
	return apps.ParseInstallFilePath(path)
}

// EnsureInstalledVersion returns the active version, creating current from the newest
// on-disk version when the link is missing (lazy migration).
func EnsureInstalledVersion(rootDir, pkgName string) (version string, ok bool) {
	ver, err := apps.EnsureCurrent(apps.PkgRoot(rootDir, pkgName))
	if err != nil || ver == "" {
		return "", false
	}
	return ver, true
}

// InstalledVersion reports the active version without mutating the current link.
func InstalledVersion(rootDir, pkgName string) (version string, ok bool) {
	return installedPackage(rootDir, pkgName)
}

// InstalledPackageVersions lists on-disk version directories for one package.
type InstalledPackageVersions struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
	Current  string   `json:"current"`
}

// ListInstalledAllVersions walks apps/ and returns each package with non-empty version dirs.
// filter limits to those package names; nil or empty means all directories under apps/.
func (e *Engine) ListInstalledAllVersions(filter []string) ([]InstalledPackageVersions, error) {
	if e == nil || e.Config == nil {
		return nil, fmt.Errorf("engine not configured")
	}
	root := e.Config.RootDir

	pkgNames, err := installedPackageNames(root, filter)
	if err != nil {
		return nil, err
	}
	sort.Strings(pkgNames)

	out := make([]InstalledPackageVersions, 0, len(pkgNames))
	for _, name := range pkgNames {
		pkg, found, err := installedPackageVersions(root, name)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		out = append(out, pkg)
	}
	return out, nil
}

// installedPackageNames resolves the packages to inspect: filter as-is when it is
// non-empty, otherwise every directory under apps/. A missing apps/ dir yields no names.
func installedPackageNames(root string, filter []string) ([]string, error) {
	if len(filter) > 0 {
		return append([]string(nil), filter...), nil
	}

	entries, err := os.ReadDir(filepath.Join(root, "apps"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read apps dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// installedPackageVersions collects the on-disk versions of one package.
// found is false when the package has no installed version, so it is skipped.
func installedPackageVersions(root, name string) (pkg InstalledPackageVersions, found bool, err error) {
	pkgRoot := apps.PkgRoot(root, name)
	versions, err := apps.ListVersions(pkgRoot)
	if err != nil {
		return InstalledPackageVersions{}, false, fmt.Errorf("list versions for %s: %w", name, err)
	}
	if len(versions) == 0 {
		return InstalledPackageVersions{}, false, nil
	}
	sort.Strings(versions)
	current, _ := apps.ReadCurrent(pkgRoot)
	return InstalledPackageVersions{
		Name:     name,
		Versions: versions,
		Current:  current,
	}, true, nil
}
