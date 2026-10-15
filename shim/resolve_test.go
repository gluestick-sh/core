package shim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveShimStubPath_cachedInGlueRoot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shim-resolve-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	stub := filepath.Join(tmpDir, "shim.exe")
	if err := os.WriteFile(stub, []byte("stub"), 0755); err != nil {
		t.Fatal(err)
	}

	got := resolveShimStubPath(tmpDir)
	if got != stub {
		t.Fatalf("resolveShimStubPath = %q, want %q", got, stub)
	}
}

func TestResolveShimStubPath_devLayout(t *testing.T) {
	repoRoot, err := os.MkdirTemp("", "shim-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repoRoot)

	stub := filepath.Join(repoRoot, "shim", "shim.exe")
	if err := os.MkdirAll(filepath.Dir(stub), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stub, []byte("stub"), 0755); err != nil {
		t.Fatal(err)
	}

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	got := resolveShimStubPath("")
	if got != stub {
		t.Fatalf("resolveShimStubPath from repo root = %q, want %q", got, stub)
	}
}

func TestWalkUpShimStub_devLayout(t *testing.T) {
	repoRoot, err := os.MkdirTemp("", "shim-repo-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(repoRoot)

	stub := filepath.Join(repoRoot, "shim", "shim.exe")
	if err := os.MkdirAll(filepath.Dir(stub), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stub, []byte("stub"), 0755); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(repoRoot, "cli", "glue")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}

	want := filepath.Clean(stub)
	for _, c := range walkUpShimStub(nested) {
		if filepath.Clean(c) == want {
			return
		}
	}
	t.Fatalf("walkUpShimStub(%q) did not include %q", nested, stub)
}

func TestCacheShimStub(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "shim-cache-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcDir, err := os.MkdirTemp("", "shim-src-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(srcDir)

	src := filepath.Join(srcDir, "shim.exe")
	if err := os.WriteFile(src, []byte("stub-bytes"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := cacheShimStub(tmpDir, src); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(tmpDir, "shim.exe")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "stub-bytes" {
		t.Fatalf("cached stub content = %q", data)
	}
}

func TestFirstExistingPath(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, shimExe)
	if err := os.WriteFile(existing, []byte("stub"), 0755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.exe")

	tests := []struct {
		name       string
		candidates []string
		want       string
	}{
		{"no candidates", nil, ""},
		{"empty entries only", []string{"", ""}, ""},
		{"missing only", []string{missing}, ""},
		{"skips empties", []string{"", missing, existing}, existing},
		{"first match wins", []string{existing, missing}, existing},
		{"duplicates are harmless", []string{existing, existing}, existing},
	}
	for _, tc := range tests {
		if got := firstExistingPath(tc.candidates); got != tc.want {
			t.Errorf("%s: firstExistingPath(%#v) = %q, want %q", tc.name, tc.candidates, got, tc.want)
		}
	}
}

func TestExecutableDir(t *testing.T) {
	dir, ok := executableDir()
	if !ok {
		t.Fatal("executableDir reported failure")
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("executableDir() = %q, want an absolute path", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("executableDir() = %q, want an existing directory (stat err: %v)", dir, err)
	}
}

func TestCacheShimStub_replacesExistingWithoutTempLeftovers(t *testing.T) {
	glueRoot := t.TempDir()
	srcDir := t.TempDir()
	dest := filepath.Join(glueRoot, shimExe)

	if err := os.WriteFile(dest, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(srcDir, shimExe)
	if err := os.WriteFile(src, []byte("new-stub"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := cacheShimStub(glueRoot, src); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-stub" {
		t.Fatalf("cached stub = %q, want new-stub", data)
	}
	if names := dirEntryNames(t, glueRoot); len(names) != 1 || names[0] != shimExe {
		t.Fatalf("cache dir = %v, want only %s", names, shimExe)
	}
}

func TestCacheShimStub_missingSourceKeepsExistingCopy(t *testing.T) {
	glueRoot := t.TempDir()
	dest := filepath.Join(glueRoot, shimExe)
	if err := os.WriteFile(dest, []byte("good"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := cacheShimStub(glueRoot, filepath.Join(glueRoot, "nope", shimExe)); err == nil {
		t.Fatal("expected an error for a missing source")
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "good" {
		t.Fatalf("existing stub = %q, want it left untouched", data)
	}
	if names := dirEntryNames(t, glueRoot); len(names) != 1 {
		t.Fatalf("cache dir = %v, want only the original %s", names, shimExe)
	}
}

func TestCacheShimStub_samePathIsNoOp(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive path comparison is Windows-specific")
	}
	glueRoot := t.TempDir()
	dest := filepath.Join(glueRoot, shimExe)
	if err := os.WriteFile(dest, []byte("keep"), 0755); err != nil {
		t.Fatal(err)
	}

	// The same file spelled with different case must not be copied onto itself.
	if err := cacheShimStub(glueRoot, strings.ToUpper(dest)); err != nil {
		t.Fatalf("cacheShimStub same path = %v, want nil", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("cached stub = %q, want keep", data)
	}
}

func dirEntryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
