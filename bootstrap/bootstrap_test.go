package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasFull7zip(t *testing.T) {
	root := t.TempDir()
	b := NewBootstrap(root)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}

	if b.hasFull7zip() {
		t.Fatal("expected false with empty bin")
	}

	if err := os.WriteFile(filepath.Join(bin, "7z.exe"), []byte("exe"), 0755); err != nil {
		t.Fatal(err)
	}
	if b.hasFull7zip() {
		t.Fatal("expected false with 7z.exe only (minimal bootstrap)")
	}

	if err := os.WriteFile(filepath.Join(bin, "7z.dll"), []byte("dll"), 0755); err != nil {
		t.Fatal(err)
	}
	if !b.hasFull7zip() {
		t.Fatal("expected true with 7z.exe and 7z.dll")
	}
}

func TestCleanupFull7zipArtifacts(t *testing.T) {
	root := t.TempDir()
	b := NewBootstrap(root)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"7zr.exe", "7z_extra.7z", "7z-installer.exe", "7z.exe", "7z.dll"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	b.cleanupFull7zipArtifacts()

	for _, name := range []string{"7zr.exe", "7z_extra.7z", "7z-installer.exe"} {
		if _, err := os.Stat(filepath.Join(bin, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed", name)
		}
	}
	for _, name := range []string{"7z.exe", "7z.dll"} {
		if _, err := os.Stat(filepath.Join(bin, name)); err != nil {
			t.Fatalf("%s should remain: %v", name, err)
		}
	}
}

func TestCleanupFull7zipArtifacts_skipsWithoutDLL(t *testing.T) {
	root := t.TempDir()
	b := NewBootstrap(root)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "7zr.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "7z.exe"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	b.cleanupFull7zipArtifacts()

	if _, err := os.Stat(filepath.Join(bin, "7zr.exe")); err != nil {
		t.Fatal("7zr.exe should remain when 7z.dll is missing")
	}
}

func TestCleanupSevenZipSeeds(t *testing.T) {
	root := t.TempDir()
	b := NewBootstrap(root)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"7zr.exe", "7z-installer.exe", "7z.exe", "7z.dll"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	b.CleanupSevenZipSeeds()

	if _, err := os.Stat(filepath.Join(bin, "7zr.exe")); !os.IsNotExist(err) {
		t.Fatal("7zr.exe should be removed by CleanupSevenZipSeeds")
	}
}

func TestCopyCodecDLLs(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "codecs")
	dest := filepath.Join(root, "out")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nsis.dll"), []byte("nsis"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := copyCodecDLLs(src, dest); got != 1 {
		t.Fatalf("copied=%d, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "nsis.dll")); err != nil {
		t.Fatalf("nsis.dll missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "readme.txt")); !os.IsNotExist(err) {
		t.Fatal("non-dll should not be copied")
	}
}

func TestEnsureDark_usesExistingBin(t *testing.T) {
	root := t.TempDir()
	b := NewBootstrap(root)
	wixDir := filepath.Join(root, "bin", "wix")
	if err := os.MkdirAll(wixDir, 0755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wixDir, "dark.exe")
	for _, name := range []string{"dark.exe", "wix.dll"} {
		if err := os.WriteFile(filepath.Join(wixDir, name), []byte(name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := b.EnsureDark(context.Background())
	if err != nil {
		t.Fatalf("EnsureDark: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnsureInnounp_usesExistingBin(t *testing.T) {
	root := t.TempDir()
	b := NewBootstrap(root)
	innounpDir := filepath.Join(root, "bin", "innounp")
	if err := os.MkdirAll(innounpDir, 0755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(innounpDir, "innounp.exe")
	if err := os.WriteFile(want, []byte("innounp"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := b.EnsureInnounp(context.Background())
	if err != nil {
		t.Fatalf("EnsureInnounp: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHasBootstrappedDark_rejectsLoneExe(t *testing.T) {
	root := t.TempDir()
	b := NewBootstrap(root)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "dark.exe"), []byte("dark"), 0755); err != nil {
		t.Fatal(err)
	}
	if b.hasBootstrappedDark() {
		t.Fatal("lone dark.exe without wix.dll should not count as ready")
	}
}

func TestDownloadToFile_fallsBackToNextMirror(t *testing.T) {
	b := NewBootstrap(t.TempDir())
	payload := []byte("archive-payload")

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer broken.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer good.Close()

	dest := filepath.Join(b.binDir(), "asset.bin")
	if err := b.downloadToFile(context.Background(), []string{broken.URL, good.URL}, dest, "asset"); err != nil {
		t.Fatalf("downloadToFile: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("content = %q, want %q", got, payload)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal("part file should be cleaned up after a successful download")
	}
}

func TestDownloadToFile_allMirrorsFail(t *testing.T) {
	b := NewBootstrap(t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	dest := filepath.Join(b.binDir(), "asset.bin")
	err := b.downloadToFile(context.Background(), []string{server.URL, server.URL}, dest, "asset")
	if err == nil {
		t.Fatal("expected error when every mirror fails")
	}
	if !strings.Contains(err.Error(), "all mirrors failed") {
		t.Fatalf("err = %v, want an all-mirrors-failed error", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("no destination file should exist after a failed download")
	}
}

func TestDownloadToFile_emptyBodyIsRejected(t *testing.T) {
	b := NewBootstrap(t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	dest := filepath.Join(b.binDir(), "asset.bin")
	if err := b.downloadToFile(context.Background(), []string{server.URL}, dest, "asset"); err == nil {
		t.Fatal("expected an empty 200 response to be treated as a failure")
	}
}

func TestDownloadToFile_noURLs(t *testing.T) {
	b := NewBootstrap(t.TempDir())
	dest := filepath.Join(b.binDir(), "asset.bin")
	err := b.downloadToFile(context.Background(), nil, dest, "asset")
	if err == nil || !strings.Contains(err.Error(), "no URLs to try") {
		t.Fatalf("err = %v, want a no-URLs error", err)
	}
}

func TestDownloadToFile_cancelledContext(t *testing.T) {
	b := NewBootstrap(t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dest := filepath.Join(b.binDir(), "asset.bin")
	if err := b.downloadToFile(ctx, []string{server.URL}, dest, "asset"); err == nil {
		t.Fatal("expected a cancelled context to abort the download")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("cancelled download should not create the destination file")
	}
}

func TestInstallFile_replacesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.exe")
	dst := filepath.Join(dir, "dst.exe")
	if err := os.WriteFile(src, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old-and-much-longer"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := installFile(src, dst); err != nil {
		t.Fatalf("installFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	if _, err := os.Stat(filepath.Join(dir, "dst.exe.part")); !os.IsNotExist(err) {
		t.Fatal("no temp files should be left behind")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("leftover temp file: %s", entry.Name())
		}
	}
}

func TestInstallFile_keepsDestinationOnMissingSource(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.exe")
	if err := os.WriteFile(dst, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := installFile(filepath.Join(dir, "missing.exe"), dst); err == nil {
		t.Fatal("expected an error for a missing source")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("destination was clobbered: %q", got)
	}
}

func TestFindExtracted(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "payload")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "7za.exe"), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	got := findExtracted(dir, "7za.exe")
	if got != filepath.Join(nested, "7za.exe") {
		t.Fatalf("got %q, want the nested 7za.exe", got)
	}
	if got := findExtracted(dir, "missing.exe"); got != "" {
		t.Fatalf("got %q, want empty for a missing file", got)
	}
}

func TestRemoveWithRetry_missingFileIsNoop(t *testing.T) {
	removeWithRetry(filepath.Join(t.TempDir(), "not-there"))
}
