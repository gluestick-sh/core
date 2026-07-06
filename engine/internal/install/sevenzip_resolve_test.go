package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gluestick-sh/core/engine/internal/runtime"
	etypes "github.com/gluestick-sh/core/engine/types"
	"github.com/gluestick-sh/core/extractor"
	"github.com/gluestick-sh/core/store"
)

func TestResolveLocalSevenZip_prefersGlueBin(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	sevenZ := filepath.Join(binDir, "7z.exe")
	if err := os.WriteFile(sevenZ, []byte("7z"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLocalSevenZip(root); got != sevenZ {
		t.Fatalf("ResolveLocalSevenZip() = %q, want %q", got, sevenZ)
	}
}

func TestResolveLocalSevenZip_prefers7zrFromInstall(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	sevenZR := filepath.Join(binDir, "7zr.exe")
	if err := os.WriteFile(sevenZR, []byte("7zr"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveLocalSevenZip(root); got != sevenZR {
		t.Fatalf("ResolveLocalSevenZip() = %q, want %q", got, sevenZR)
	}
}

func TestSetSevenZipFromLocal_skipsBootstrap(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	sevenZ := filepath.Join(binDir, "7z.exe")
	if err := os.WriteFile(sevenZ, []byte("7z"), 0755); err != nil {
		t.Fatal(err)
	}
	store, err := store.NewStore(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	e := &runtime.Engine{
		Config:    &etypes.EngineConfig{RootDir: root},
		Extractor: extractor.NewExtractor(store),
	}
	if !SetSevenZipFromLocal(e) {
		t.Fatal("expected local 7z to be configured")
	}
	if e.Extractor.SevenZipPath() != sevenZ {
		t.Fatalf("SevenZipPath = %q, want %q", e.Extractor.SevenZipPath(), sevenZ)
	}
}
