package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/shim"
)

func TestCreatePackageShims_skipsConflictingShimName(t *testing.T) {
	root := t.TempDir()
	appsDir := filepath.Join(root, "apps")
	nodeInstall := filepath.Join(appsDir, "nodejs", "22.0.0")
	ltsInstall := filepath.Join(appsDir, "nodejs-lts", "20.0.0")
	for _, dir := range []string{nodeInstall, ltsInstall} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "node.exe"), []byte("node"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	shimMgr, err := shim.NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	shimsMetaDir := filepath.Join(root, "shims-meta")
	stub := filepath.Join(root, "shim.exe")
	if err := os.WriteFile(stub, []byte("stub"), 0755); err != nil {
		t.Fatal(err)
	}

	nodeCurrent := filepath.Join(nodeInstall, "node.exe")
	if err := shimMgr.Create("node", nodeCurrent); err != nil {
		t.Fatalf("seed nodejs shim: %v", err)
	}

	ltsCurrent := filepath.Join(ltsInstall, "node.exe")
	m := &manifest.Manifest{Bin: []interface{}{"node.exe"}}
	if err := createPackageShims(shimMgr, shimsMetaDir, "nodejs-lts", ltsInstall, ltsCurrent, m); err != nil {
		t.Fatalf("createPackageShims: %v", err)
	}

	owner, conflict := shim.OwnedByOtherPackage(shimsMetaDir, "node", "nodejs-lts")
	if !conflict || owner != "nodejs" {
		t.Fatalf("node shim owner = %q conflict=%v, want nodejs/true", owner, conflict)
	}
}
