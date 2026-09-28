package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gluestick-sh/core/config"
	"github.com/gluestick-sh/core/verbose"
)

const (
	darkExe             = "dark.exe"
	innounpExe          = "innounp.exe"
	darkBootstrapURL    = "https://raw.githubusercontent.com/ScoopInstaller/Binary/master/dark/dark-3.14.1.zip"
	innounpBootstrapURL = "https://raw.githubusercontent.com/jrathlev/InnoUnpacker-Windows-GUI/refs/heads/master/innounp-2/bin/innounp-2.zip"
)

// zipTool describes a tool shipped as a zip that is unpacked into its own
// subdirectory of bin. WiX and innounp differ only in their asset and layout,
// so they share one install path.
type zipTool struct {
	name     string   // progress label
	urls     []string // download mirrors, tried in order
	archive  string   // zip file name inside bin
	destDir  string   // bin subdirectory the tool is unpacked into
	required []string // files that must exist in destDir afterwards
	exe      string   // entry-point executable, relative to destDir
}

// install downloads the tool's zip, unpacks it into destDir with 7z, and
// returns the path of its executable. destDir is only replaced once every
// required file is present, so an interrupted run leaves no partial install.
func (t zipTool) install(ctx context.Context, b *Bootstrap) (string, error) {
	archive, err := b.ensureArchive(ctx, t.archive, t.urls, t.name)
	if err != nil {
		return "", err
	}
	defer removeWithRetry(archive)

	verbose.Progressf("Bootstrapping: extracting %s...\n", t.name)
	sevenZip, err := b.Ensure7z(ctx)
	if err != nil {
		return "", fmt.Errorf("ensure 7z: %w", err)
	}

	destDir := filepath.Join(b.binDir(), t.destDir)
	staging, cleanup, err := b.newExtractDir()
	if err != nil {
		return "", err
	}
	defer cleanup()

	if err := extractWith7z(ctx, sevenZip, archive, staging); err != nil {
		// A leftover archive from an earlier (possibly truncated) run would fail
		// the same way forever; drop it so the next attempt re-downloads.
		removeWithRetry(archive)
		return "", err
	}
	for _, name := range t.required {
		if !fileExists(filepath.Join(staging, name)) {
			return "", fmt.Errorf("%s missing after extraction", name)
		}
	}
	if err := replaceDir(staging, destDir); err != nil {
		return "", err
	}

	verbose.Progressf("  Bootstrapped %s to %s\n", t.name, destDir)
	return filepath.Join(destDir, t.exe), nil
}

func (b *Bootstrap) wixDir() string {
	return filepath.Join(b.binDir(), "wix")
}

func (b *Bootstrap) bootstrappedDarkPath() string {
	return filepath.Join(b.wixDir(), darkExe)
}

// hasBootstrappedDark reports whether dark.exe and its required wix.dll are both
// present; a lone dark.exe cannot run.
func (b *Bootstrap) hasBootstrappedDark() bool {
	wixDir := b.wixDir()
	return fileExists(filepath.Join(wixDir, darkExe)) && fileExists(filepath.Join(wixDir, "wix.dll"))
}

// darkTool is the WiX 3 toolset bootstrap.
func (b *Bootstrap) darkTool() zipTool {
	return zipTool{
		name:     "WiX toolset (dark)",
		urls:     config.MirrorURLs(darkBootstrapURL, b.ghProxies),
		archive:  "dark.zip",
		destDir:  "wix",
		required: []string{darkExe, "wix.dll"},
		exe:      darkExe,
	}
}

// EnsureDark ensures the WiX 3 toolset (dark.exe + wix.dll) is available under glue/bin/wix.
func (b *Bootstrap) EnsureDark(ctx context.Context) (string, error) {
	return withToolLock(ctx, darkLock(b.rootDir), func(ctx context.Context) (string, error) {
		if b.hasBootstrappedDark() {
			return b.bootstrappedDarkPath(), nil
		}
		// Older glue versions unpacked the toolset straight into bin.
		removeWithRetry(filepath.Join(b.binDir(), darkExe))
		return b.darkTool().install(ctx, b)
	})
}

func (b *Bootstrap) innounpDir() string {
	return filepath.Join(b.binDir(), "innounp")
}

func (b *Bootstrap) bootstrappedInnounpPath() string {
	return filepath.Join(b.innounpDir(), "innounp.exe")
}

// hasBootstrappedInnounp reports whether innounp.exe is present.
func (b *Bootstrap) hasBootstrappedInnounp() bool {
	return fileExists(b.bootstrappedInnounpPath())
}

// innounpTool is the Inno Setup unpacker bootstrap.
func (b *Bootstrap) innounpTool() zipTool {
	return zipTool{
		name:     "innounp",
		urls:     config.MirrorURLs(innounpBootstrapURL, b.ghProxies),
		archive:  "innounp.zip",
		destDir:  "innounp",
		required: []string{innounpExe},
		exe:      innounpExe,
	}
}

// EnsureInnounp ensures innounp.exe is available under glue/bin/innounp.
func (b *Bootstrap) EnsureInnounp(ctx context.Context) (string, error) {
	return withToolLock(ctx, innounpLock(b.rootDir), func(ctx context.Context) (string, error) {
		if b.hasBootstrappedInnounp() {
			return b.bootstrappedInnounpPath(), nil
		}
		return b.innounpTool().install(ctx, b)
	})
}
