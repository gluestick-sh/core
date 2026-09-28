package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gluestick-sh/core/config"
	"github.com/gluestick-sh/core/verbose"
)

const (
	// minGitVersion is the MinGit release bootstrapped by default. The release tag
	// and both asset URLs below must name the same version.
	minGitVersion = "2.54.0"
	minGitRelease = "v" + minGitVersion + ".windows.1"
	minGitAsset   = "MinGit-" + minGitVersion + "-64-bit.zip"
)

func (b *Bootstrap) minGitZipPath() string {
	return filepath.Join(b.binDir(), "mingit.zip")
}

func (b *Bootstrap) cleanupMinGitZip() {
	removeWithRetry(b.minGitZipPath())
}

// bootstrappedGitPath is where the installed git.exe lives, matching MinGit's
// own layout (mingw64/bin) so relative lookups inside git keep working.
func (b *Bootstrap) bootstrappedGitPath() string {
	return filepath.Join(b.binDir(), "git", "mingw64", "bin", "git.exe")
}

// minGitURLs lists the CDN first (usually fastest from CN), then the GitHub
// release and any configured mirrors.
func (b *Bootstrap) minGitURLs() []string {
	cdn := "https://registry.npmmirror.com/-/binary/git-for-windows/" + minGitRelease + "/" + minGitAsset
	github := "https://github.com/git-for-windows/git/releases/download/" + minGitRelease + "/" + minGitAsset
	return append([]string{cdn}, config.MirrorURLs(github, b.ghProxies)...)
}

// EnsureGit ensures git is available in glue's bin directory
func (b *Bootstrap) EnsureGit(ctx context.Context) (string, error) {
	return withToolLock(ctx, gitLock(b.rootDir), b.ensureGit)
}

func (b *Bootstrap) ensureGit(ctx context.Context) (string, error) {
	gitExe := b.bootstrappedGitPath()
	if GitExecutableReady(gitExe) {
		b.cleanupMinGitZip()
		return gitExe, nil
	}
	return b.downloadMinGit(ctx)
}

// downloadMinGit downloads a minimal Git for Windows distribution and installs
// it through a staging directory, so a failed extraction never replaces a
// working git with a half-extracted one.
func (b *Bootstrap) downloadMinGit(ctx context.Context) (string, error) {
	binDir := b.binDir()
	if err := os.MkdirAll(binDir, dataFileMode); err != nil {
		return "", fmt.Errorf("create %s: %w", binDir, err)
	}

	verbose.Progressf("Bootstrapping: downloading MinGit...\n")
	archive, err := b.ensureArchive(ctx, "mingit.zip", b.minGitURLs(), "MinGit")
	if err != nil {
		return "", err
	}
	defer b.cleanupMinGitZip()

	verbose.Progressf("Bootstrapping: extracting MinGit...\n")
	sevenZip, err := b.Ensure7z(ctx)
	if err != nil {
		return "", fmt.Errorf("ensure 7z: %w", err)
	}

	staging, cleanup, err := b.newExtractDir()
	if err != nil {
		return "", err
	}
	defer func() {
		// After a successful swap the staging dir no longer exists; RemoveAll
		// on the missing path is a no-op, so one cleanup covers both outcomes.
		cleanup()
	}()

	if err := extractWith7z(ctx, sevenZip, archive, staging); err != nil {
		return "", fmt.Errorf("extract mingit: %w", err)
	}
	stagedGit := filepath.Join(staging, "mingw64", "bin", "git.exe")
	if !fileExists(stagedGit) {
		return "", fmt.Errorf("git.exe not found in staging extract")
	}

	gitDir := filepath.Join(binDir, "git")
	if err := replaceDir(staging, gitDir); err != nil {
		return "", err
	}

	gitExe := b.bootstrappedGitPath()
	if !GitExecutableReady(gitExe) {
		return "", fmt.Errorf("git.exe not runnable after extraction")
	}

	verbose.Progressf("  Bootstrapped MinGit to %s\n", gitDir)
	return gitExe, nil
}
