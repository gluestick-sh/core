package shim

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	shimExe = "shim.exe"
)

// resolveShimStubPath returns the path to the prebuilt shim runner (shim.exe), or "" if not found.
func resolveShimStubPath(glueRootDir string) string {
	var candidates []string

	if glueRootDir != "" {
		candidates = append(candidates, filepath.Join(glueRootDir, shimExe))
	}

	if execDir, ok := executableDir(); ok {
		candidates = append(candidates, filepath.Join(execDir, shimExe))
		candidates = append(candidates, walkUpShimStub(execDir)...)
	}

	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		candidates = append(candidates, devShimStubCandidates(cwd)...)
	}

	if pd := os.Getenv("PROGRAMDATA"); pd != "" {
		candidates = append(candidates, filepath.Join(pd, "glue", shimExe))
	}

	return firstExistingPath(candidates)
}

// executableDir returns the directory holding the running executable with
// symlinks resolved. ok is false when the path cannot be determined.
func executableDir() (dir string, ok bool) {
	execPath, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}
	return filepath.Dir(execPath), true
}

// firstExistingPath returns the first candidate that exists on disk, skipping
// empty entries and duplicates. It returns "" when nothing matches.
func firstExistingPath(candidates []string) string {
	seen := make(map[string]struct{}, len(candidates))
	for _, p := range candidates {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// devShimStubCandidates returns local dev paths for a prebuilt shim.exe under dir.
func devShimStubCandidates(dir string) []string {
	return []string{
		filepath.Join(dir, "shim", shimExe),
		filepath.Join(dir, shimExe),
	}
}

func walkUpShimStub(startDir string) []string {
	var out []string
	dir := startDir
	for range 8 {
		out = append(out, devShimStubCandidates(dir)...)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

// cacheShimStub copies stub to glueRoot/shim.exe so later installs do not depend on cwd.
// The copy is staged beside the destination and renamed into place, so an
// interrupted copy never leaves a truncated shim runner behind.
func cacheShimStub(glueRootDir, stubPath string) error {
	if glueRootDir == "" || stubPath == "" {
		return nil
	}
	dest := filepath.Join(glueRootDir, shimExe)
	if strings.EqualFold(filepath.Clean(dest), filepath.Clean(stubPath)) {
		return nil
	}
	return copyFileAtomic(stubPath, dest, 0755)
}

// copyFileAtomic stages src next to dst and renames it into place, replacing dst
// only once the whole file is on disk. Windows refuses the rename while a process
// is running dst, so a failed rename drops dst and retries once.
func copyFileAtomic(src, dst string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // already gone once the rename succeeds

	if err := copyStream(tmp, src); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpPath, dst)
}

// copyStream writes src into the open file dst and closes dst.
func copyStream(dst *os.File, src string) error {
	in, err := os.Open(src)
	if err != nil {
		dst.Close()
		return err
	}
	defer in.Close()

	if _, err := io.Copy(dst, in); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
