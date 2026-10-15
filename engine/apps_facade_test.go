package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gluestick-sh/core/apps"
)

func TestListInstalledAllVersions(t *testing.T) {
	root := t.TempDir()
	pnpmRoot := apps.PkgRoot(root, "pnpm")
	for _, ver := range []string{"11.6.0", "11.7.0"} {
		dir := filepath.Join(pnpmRoot, ver)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stub"), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := apps.LinkCurrent(pnpmRoot, "11.7.0"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(apps.PkgRoot(root, "freecad"), 0755); err != nil {
		t.Fatal(err)
	}

	eng, err := NewEngine(&EngineConfig{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	got, err := eng.ListInstalledAllVersions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "pnpm" || got[0].Current != "11.7.0" {
		t.Fatalf("ListInstalledAllVersions = %#v", got)
	}
}

// installStubPackage creates apps/<pkg>/<version> with a marker file so the
// version dir counts as installed.
func installStubPackage(t *testing.T, root, pkg, ver string) {
	t.Helper()
	dir := filepath.Join(apps.PkgRoot(root, pkg), ver)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stub"), nil, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestListInstalledAllVersions_Filter(t *testing.T) {
	root := t.TempDir()
	installStubPackage(t, root, "pnpm", "11.7.0")
	installStubPackage(t, root, "vim", "9.2")
	// freecad has no version dirs, so a filter naming it must yield nothing.
	if err := os.MkdirAll(apps.PkgRoot(root, "freecad"), 0755); err != nil {
		t.Fatal(err)
	}

	eng, err := NewEngine(&EngineConfig{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	got, err := eng.ListInstalledAllVersions([]string{"vim", "pnpm", "freecad", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListInstalledAllVersions(filter) = %#v, want 2 entries", got)
	}
	// Results stay sorted by name even when the filter is not.
	if got[0].Name != "pnpm" || got[1].Name != "vim" {
		t.Fatalf("names not sorted: %#v", got)
	}
}

func TestListInstalledAllVersions_MissingAppsDir(t *testing.T) {
	eng, err := NewEngine(&EngineConfig{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	got, err := eng.ListInstalledAllVersions(nil)
	if err != nil {
		t.Fatalf("ListInstalledAllVersions: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListInstalledAllVersions = %#v, want no packages", got)
	}
}

// TestInstalledPackageNames_MissingAppsDir covers the missing apps/ branch that
// NewEngine masks by creating the layout up front.
func TestInstalledPackageNames_MissingAppsDir(t *testing.T) {
	got, err := installedPackageNames(filepath.Join(t.TempDir(), "absent"), nil)
	if err != nil || got != nil {
		t.Fatalf("installedPackageNames = (%#v, %v), want (nil, nil)", got, err)
	}
}

func TestEnsureInstalledVersion(t *testing.T) {
	root := t.TempDir()
	pkgRoot := apps.PkgRoot(root, "vim")
	dir := filepath.Join(pkgRoot, "9.2")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vim.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	ver, ok := EnsureInstalledVersion(root, "vim")
	if !ok || ver != "9.2" {
		t.Fatalf("EnsureInstalledVersion = (%q, %v)", ver, ok)
	}
	current, err := apps.ReadCurrent(pkgRoot)
	if err != nil || current != "9.2" {
		t.Fatalf("current = %q, err = %v", current, err)
	}
}
