package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gluestick-sh/core/procutil"
	"github.com/gluestick-sh/core/verbose"
)

const (
	// sevenZipVersion is the 7-Zip release bootstrapped by default. The extra
	// package and the x64 installer below must name the same release.
	sevenZipVersion        = "2601"
	sevenZipDll            = "7z.dll"
	sevenZipExe            = "7z.exe"
	sevenZipRExe           = "7zr.exe"
	sevenZipExtraAsset     = "7z" + sevenZipVersion + "-extra.7z"
	sevenZipInstallerAsset = "7z" + sevenZipVersion + "-x64.exe"
)

// sevenZipURLs returns the upstream 7-Zip hosts for asset, in preference order.
func sevenZipURLs(asset string) []string {
	return []string{
		"https://www.7-zip.org/a/" + asset,
		"https://mirror.7-zip.org/a/" + asset,
	}
}

func (b *Bootstrap) sevenZipPath() string {
	return filepath.Join(b.binDir(), sevenZipExe)
}

// verify7zRunnable reports whether path runs as a working 7-Zip binary.
func verify7zRunnable(path string) error {
	cmd := exec.Command(path, "i")
	procutil.HideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("7z not runnable: %w\n%s", err, stderr.String())
	}
	return nil
}

// ensure7zExtractor returns a 7-Zip binary for bootstrap extraction.
// Prefers bin/7z.exe when present; otherwise downloads minimal 7zr.exe.
func (b *Bootstrap) ensure7zExtractor(ctx context.Context, destDir string) (string, error) {
	for _, name := range []string{sevenZipExe, sevenZipRExe} {
		candidate := filepath.Join(destDir, name)
		if !fileExists(candidate) {
			continue
		}
		if err := verify7zRunnable(candidate); err == nil {
			return candidate, nil
		}
	}
	return b.ensure7zr(ctx, destDir)
}

// ensure7zr returns bin/7zr.exe, downloading it when absent or unusable.
// 7zr.exe is the reduced build: it reads 7z archives but cannot write them,
// which is all bootstrap needs to unpack the full installer later.
func (b *Bootstrap) ensure7zr(ctx context.Context, destDir string) (string, error) {
	sevenZipR := filepath.Join(destDir, sevenZipRExe)
	if fileExists(sevenZipR) && verify7zRunnable(sevenZipR) == nil {
		return sevenZipR, nil
	}

	verbose.Progressf("Bootstrapping: downloading 7zr.exe...\n")
	if err := b.downloadToFile(ctx, sevenZipURLs(sevenZipRExe), sevenZipR, sevenZipRExe); err != nil {
		return "", fmt.Errorf("download 7zr: %w", err)
	}
	if err := os.Chmod(sevenZipR, execFileMode); err != nil {
		return "", fmt.Errorf("make 7zr.exe executable: %w", err)
	}
	verbose.Progressf("  Downloaded 7zr.exe\n")
	return sevenZipR, nil
}

// extractWith7z unpacks archive into outDir with 7z, reporting 7z's stderr on
// failure. ctx cancellation kills the child process.
func extractWith7z(ctx context.Context, extractor, archive, outDir string) error {
	cmd := exec.CommandContext(ctx, extractor, "x", "-o"+outDir, "-y", archive)
	procutil.HideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extract %s: %w\n%s", filepath.Base(archive), err, stderr.String())
	}
	return nil
}

// newExtractDir creates a unique scratch directory under bin for unpacking an
// archive, and returns it with a cleanup func. A unique name keeps concurrent
// bootstraps and leftovers from a crashed run from colliding.
func (b *Bootstrap) newExtractDir() (string, func(), error) {
	dir, err := os.MkdirTemp(b.binDir(), "extract-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// sevenZipSeeds are download-only intermediates that can go once a working
// 7z.exe is in place.
var sevenZipSeeds = []string{sevenZipRExe, "7z_extra.7z", "7z-installer.exe"}

// CleanupSevenZipSeeds removes bootstrap-only files when a usable 7z.exe is present.
func (b *Bootstrap) CleanupSevenZipSeeds() {
	if b.hasFull7zip() {
		b.cleanupFull7zipArtifacts()
		return
	}
	b.cleanupBootstrapArtifacts()
}

// cleanupBootstrapArtifacts removes seed files after minimal 7z.exe (7za) is ready.
// 7z-installer.exe is kept: it is what promotes the minimal build to the full
// codec build without a re-download.
func (b *Bootstrap) cleanupBootstrapArtifacts() {
	sevenZ := b.sevenZipPath()
	if !fileExists(sevenZ) {
		return
	}
	if err := verify7zRunnable(sevenZ); err != nil {
		return
	}
	for _, name := range sevenZipSeeds[:2] {
		removeWithRetry(filepath.Join(b.binDir(), name))
	}
}

// cleanupFull7zipArtifacts removes bootstrap intermediates once 7z.exe + 7z.dll are present.
func (b *Bootstrap) cleanupFull7zipArtifacts() {
	if !b.hasFull7zip() {
		return
	}
	for _, name := range sevenZipSeeds {
		removeWithRetry(filepath.Join(b.binDir(), name))
	}
}

// hasFull7zip reports whether the full installer build is present (not the minimal 7za bootstrap).
func (b *Bootstrap) hasFull7zip() bool {
	bin := b.binDir()
	return fileExists(filepath.Join(bin, sevenZipExe)) && fileExists(filepath.Join(bin, sevenZipDll))
}

// Ensure7z ensures 7z.exe is available in glue's bin directory.
// Downloads the full 7-Zip console version (7za.exe) which supports .zip, .7z, .rar, .tar, .gz, etc.
func (b *Bootstrap) Ensure7z(ctx context.Context) (string, error) {
	return withToolLock(ctx, sevenZipLock(b.rootDir), b.ensure7z)
}

func (b *Bootstrap) ensure7z(ctx context.Context) (string, error) {
	destPath := b.sevenZipPath()
	if fileExists(destPath) {
		b.cleanupBootstrapArtifacts()
		return destPath, nil
	}
	return b.download7z(ctx)
}

// Ensure7zip ensures the full 7-Zip console build is available (7z.exe + 7z.dll).
// 7-Zip 26+ bundles NSIS/ISO/RAR codecs in 7z.dll; older installers may ship codecs/*.dll.
func (b *Bootstrap) Ensure7zip(ctx context.Context) (string, error) {
	return withToolLock(ctx, sevenZipLock(b.rootDir), b.ensure7zip)
}

func (b *Bootstrap) ensure7zip(ctx context.Context) (string, error) {
	destPath := b.sevenZipPath()
	if b.hasFull7zip() {
		b.cleanupFull7zipArtifacts()
		return destPath, nil
	}
	return b.downloadFull7zip(ctx)
}

// download7z downloads the full 7-Zip (7za.exe) which supports .zip and other formats.
// Process: download 7z_extra.7z -> extract 7za.exe with 7zr.exe -> install as 7z.exe.
func (b *Bootstrap) download7z(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	destDir := b.binDir()
	if err := os.MkdirAll(destDir, dataFileMode); err != nil {
		return "", fmt.Errorf("create %s: %w", destDir, err)
	}
	destPath := b.sevenZipPath()

	// Step 1: the Extra package holds 7za.exe, the build that handles .zip.
	extra7z, err := b.ensureArchive(ctx, "7z_extra.7z", sevenZipURLs(sevenZipExtraAsset), "7-Zip Extra")
	if err != nil {
		return "", err
	}

	// Step 2: extract 7za.exe from the Extra package.
	verbose.Progressf("Bootstrapping: extracting 7za.exe...\n")
	extractor, err := b.ensure7zExtractor(ctx, destDir)
	if err != nil {
		return "", err
	}
	tempDir, cleanup, err := b.newExtractDir()
	if err != nil {
		return "", err
	}
	defer cleanup()

	if err := extractWith7z(ctx, extractor, extra7z, tempDir); err != nil {
		// A cached archive that no longer extracts is stale or truncated; drop it
		// so the next run downloads it again instead of failing forever.
		removeWithRetry(extra7z)
		return "", fmt.Errorf("extract 7z_extra: %w", err)
	}
	extracted := findExtracted(tempDir, "7za.exe")
	if extracted == "" {
		return "", fmt.Errorf("7za.exe not found in extracted archive")
	}
	if err := installFile(extracted, destPath); err != nil {
		return "", fmt.Errorf("write 7z.exe: %w", err)
	}

	verbose.Progressf("  Bootstrapped 7z.exe\n")
	b.cleanupBootstrapArtifacts()
	return destPath, nil
}

// downloadFull7zip downloads the full 7-Zip installer and extracts it with all codecs.
// The installer is a self-extracting archive, so a 7zr seed may still be needed
// to unpack it even when 7z.exe is absent.
func (b *Bootstrap) downloadFull7zip(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	destDir := b.binDir()
	if err := os.MkdirAll(destDir, dataFileMode); err != nil {
		return "", fmt.Errorf("create %s: %w", destDir, err)
	}
	destPath := b.sevenZipPath()
	codecsDir := filepath.Join(destDir, "codecs")

	installer, err := b.ensureArchive(ctx, "7z-installer.exe", sevenZipURLs(sevenZipInstallerAsset), "full 7-Zip installer")
	if err != nil {
		return "", err
	}

	verbose.Progressf("Bootstrapping: extracting 7-Zip...\n")
	extractor, err := b.ensure7zExtractor(ctx, destDir)
	if err != nil {
		return "", err
	}
	tempDir, cleanup, err := b.newExtractDir()
	if err != nil {
		return "", err
	}
	defer cleanup()

	if err := extractWith7z(ctx, extractor, installer, tempDir); err != nil {
		// Same self-healing as download7z: a cached installer that will not
		// unpack must not wedge every future run.
		removeWithRetry(installer)
		return "", fmt.Errorf("extract 7z installer: %w", err)
	}
	extracted := findExtracted(tempDir, sevenZipExe)
	if extracted == "" {
		return "", fmt.Errorf("7z.exe not found in extracted installer")
	}
	payloadDir := filepath.Dir(extracted)

	sevenZipDll := filepath.Join(payloadDir, sevenZipDll)
	if !fileExists(sevenZipDll) {
		return "", fmt.Errorf("7z.dll not found in extracted installer")
	}
	// Install the DLL before the exe: a half-upgraded 7z.exe without its codecs
	// fails at extraction time with a much less obvious error.
	if err := copyFile(sevenZipDll, filepath.Join(destDir, sevenZipDll)); err != nil {
		return "", fmt.Errorf("write 7z.dll: %w", err)
	}
	if err := installFile(extracted, destPath); err != nil {
		return "", fmt.Errorf("write 7z.exe: %w", err)
	}

	if b.installCodecs(payloadDir, tempDir, codecsDir) == 0 {
		_ = os.RemoveAll(codecsDir)
	}

	verbose.Progressf("  Bootstrapped full 7-Zip\n")
	b.cleanupFull7zipArtifacts()
	return destPath, nil
}

// installCodecs copies the installer's codec DLLs into destDir and reports how
// many landed. 7-Zip 26+ bundles codecs in 7z.dll, so zero is a valid outcome.
func (b *Bootstrap) installCodecs(payloadDir, tempDir, destDir string) int {
	if n := copyCodecDLLs(filepath.Join(payloadDir, "codecs"), destDir); n > 0 {
		return n
	}
	// Older layouts may nest codecs elsewhere in the SFX payload.
	copied := 0
	_ = filepath.WalkDir(tempDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == tempDir {
			return err
		}
		if strings.EqualFold(d.Name(), "codecs") {
			copied += copyCodecDLLs(path, destDir)
		}
		return nil
	})
	return copied
}

// copyCodecDLLs copies the .dll files in srcDir into destDir and reports how many
// were copied. Non-DLL entries and unreadable files are skipped.
func copyCodecDLLs(srcDir, destDir string) int {
	codecInfos, err := os.ReadDir(srcDir)
	if err != nil {
		return 0
	}
	var copied int
	for _, codecInfo := range codecInfos {
		if codecInfo.IsDir() || !strings.EqualFold(filepath.Ext(codecInfo.Name()), ".dll") {
			continue
		}
		if err := copyFile(filepath.Join(srcDir, codecInfo.Name()), filepath.Join(destDir, codecInfo.Name())); err != nil {
			continue
		}
		copied++
	}
	return copied
}
