// Bin helpers shared by shim creation, install, and launch: parsing manifest
// `bin` entries (Scoop semantics) and resolving them to on-disk candidates.
// Living here is what lets install and launch avoid importing each other.

package shim

import (
	"os"
	"path/filepath"
	"strings"
)

// ParseBinPattern parses Scoop bin entries into executable and alias.
// This is a simplified version that returns just the exe and alias,
// ignoring any additional default arguments.
// Parameters:
//   - pattern: Bin pattern string (e.g., "exe", "exe,alias", "[exe,alias,args]")
//
// Returns:
//   - exe: Executable name/path
//   - alias: Shim alias (if any)
func ParseBinPattern(pattern string) (exe, alias string) {
	exe, alias, _ = ParseBinPatternParts(pattern)
	return exe, alias
}

// ParseBinPatternParts parses Scoop bin entries including optional default arguments.
// Supports multiple formats:
// - "exe" - just the executable
// - "exe,alias" - executable with shim alias
// - "[exe,alias,arg1,arg2]" - executable, alias, and default arguments for shim
//
// Parameters:
//   - pattern: Bin pattern string
//
// Returns:
//   - exe: Executable name/path
//   - alias: Shim alias (empty if not specified)
//   - extraArgs: Default arguments to pass through shim (empty if none)
func ParseBinPatternParts(pattern string) (exe, alias string, extraArgs []string) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", nil
	}

	var parts []string
	switch {
	case strings.HasPrefix(pattern, "["):
		inner := strings.Trim(pattern, "[]")
		for _, p := range strings.Split(inner, ",") {
			parts = append(parts, trimBinToken(p))
		}
	case strings.Contains(pattern, ","):
		for _, p := range strings.Split(pattern, ",") {
			parts = append(parts, trimBinToken(p))
		}
	default:
		return trimBinToken(pattern), "", nil
	}

	if len(parts) == 0 || parts[0] == "" {
		return "", "", nil
	}
	exe = parts[0]
	if len(parts) > 1 {
		alias = parts[1]
	}
	if len(parts) > 2 {
		extraArgs = append([]string(nil), parts[2:]...)
	}
	return exe, alias, extraArgs
}

// trimBinToken removes surrounding whitespace and quotes from a bin token.
// Handles both single and double quotes.
// Parameters:
//   - s: String to trim
//
// Returns: trimmed string without quotes.
func trimBinToken(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return strings.Trim(s, "\"'")
}

// ShouldSkipPortableScanDir reports whether a directory name must be skipped when
// scanning an install directory for binaries (VCS/tooling dirs and dot-dirs).
func ShouldSkipPortableScanDir(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "node_modules", ".git", "__pycache__":
		return true
	default:
		return strings.HasPrefix(name, ".")
	}
}

// ResolveBinCandidates returns possible on-disk locations for a manifest bin, checking the
// install root and one level of subdirectories (including Scoop's "_"-prefixed variants).
func ResolveBinCandidates(installDir, binName, downloadName string) []string {
	out := InstallRootBinCandidates(installDir, binName, downloadName)
	seen := make(map[string]struct{}, len(out))
	for _, p := range out {
		seen[p] = struct{}{}
	}
	if binName != "" {
		entries, err := os.ReadDir(installDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() || ShouldSkipPortableScanDir(entry.Name()) {
					continue
				}
				for _, rel := range []string{binName, "_" + binName} {
					path := filepath.Join(installDir, entry.Name(), rel)
					if _, ok := seen[path]; ok {
						continue
					}
					if _, err := os.Stat(path); err != nil {
						continue
					}
					seen[path] = struct{}{}
					out = append(out, path)
				}
			}
		}
	}
	return out
}

// InstallRootBinCandidates returns bin candidates directly under the install root,
// including Scoop's "_"-prefixed variant, SourceDir, and the download name.
func InstallRootBinCandidates(installDir, binName, downloadName string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		if _, err := os.Stat(path); err != nil {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	if binName != "" {
		add(filepath.Join(installDir, binName))
		add(filepath.Join(installDir, "_"+binName))
		add(filepath.Join(installDir, "SourceDir", binName))
	}
	if downloadName != "" {
		add(filepath.Join(installDir, downloadName))
	}
	return out
}
