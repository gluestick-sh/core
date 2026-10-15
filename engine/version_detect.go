package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gluestick-sh/core/apps"
	"github.com/gluestick-sh/core/manifest"
	"github.com/gluestick-sh/core/procutil"
)

const (
	versionProbeSourceJSON        = "json"
	versionProbeSourceFileVersion = "fileversion"
	versionProbeSourceCommand     = "command"
	versionProbeSourceDir         = "version-dir"
	versionProbeCommandTimeout    = 2 * time.Second
)

type sidecarVersionSpec struct {
	file string
	key  string
}

// Known sidecar files whose "version" reflects the product release, not a bundled engine.
var defaultSidecarVersionFiles = []sidecarVersionSpec{
	{file: "tbb_version.json", key: "version"}, // Tor Browser
}

// VersionDetectResult is the managed vs runtime-detected version for an install.
type VersionDetectResult struct {
	ManagedVersion    string `json:"managedVersion"`
	DetectedVersion   string `json:"detectedVersion,omitempty"`
	Source            string `json:"source,omitempty"`
	ExternallyUpdated bool   `json:"externallyUpdated,omitempty"`
}

// DetectInstalledVersion probes the install directory for a runtime version.
// Sidecar JSON (e.g. tbb_version.json) is preferred over PE FileVersion because
// many apps embed a different engine version in the executable (Firefox ESR in Tor Browser).
func DetectInstalledVersion(installDir, managedVersion string, m *manifest.Manifest) VersionDetectResult {
	result := VersionDetectResult{ManagedVersion: strings.TrimSpace(managedVersion)}
	if installDir == "" {
		return result
	}

	if ver, ok := detectSidecarJSONVersion(installDir, m); ok {
		result.DetectedVersion = ver
		result.Source = versionProbeSourceJSON
	} else {
		strategies := versionProbeStrategies(m)
		candidates := versionProbeCandidates(installDir, m)
		for _, strategy := range strategies {
			switch strategy {
			case versionProbeSourceFileVersion:
				if ver, ok := detectFileVersion(candidates); ok && versionsCompatibleForDrift(managedVersion, ver) {
					result.DetectedVersion = ver
					result.Source = versionProbeSourceFileVersion
				}
			case versionProbeSourceCommand:
				if result.DetectedVersion != "" {
					continue
				}
				if ver, ok := detectCommandVersion(candidates, m); ok && versionsCompatibleForDrift(managedVersion, ver) {
					result.DetectedVersion = ver
					result.Source = versionProbeSourceCommand
				}
			}
			if result.DetectedVersion != "" {
				break
			}
		}

		if result.DetectedVersion == "" {
			if ver, ok := detectNestedVersionDir(installDir); ok && versionsCompatibleForDrift(managedVersion, ver) {
				result.DetectedVersion = ver
				result.Source = versionProbeSourceDir
			}
		}
	}

	if result.DetectedVersion != "" && result.ManagedVersion != "" &&
		versionsCompatibleForDrift(result.ManagedVersion, result.DetectedVersion) &&
		versionCompare(result.DetectedVersion, result.ManagedVersion) > 0 {
		result.ExternallyUpdated = true
	}
	return result
}

func versionProbeStrategies(m *manifest.Manifest) []string {
	if m != nil && m.VersionProbe != nil {
		if len(m.VersionProbe.Strategies) > 0 {
			out := make([]string, 0, len(m.VersionProbe.Strategies))
			for _, s := range m.VersionProbe.Strategies {
				s = strings.ToLower(strings.TrimSpace(s))
				switch s {
				case versionProbeSourceJSON, versionProbeSourceFileVersion, versionProbeSourceCommand:
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		if m.VersionProbe.File != "" || len(m.VersionProbe.Args) > 0 || m.VersionProbe.Pattern != "" {
			if strings.HasSuffix(strings.ToLower(m.VersionProbe.File), ".json") {
				return []string{versionProbeSourceJSON, versionProbeSourceFileVersion, versionProbeSourceCommand}
			}
			return []string{versionProbeSourceFileVersion, versionProbeSourceCommand}
		}
	}
	return []string{versionProbeSourceFileVersion}
}

func sidecarSpecs(m *manifest.Manifest) []sidecarVersionSpec {
	specs := append([]sidecarVersionSpec(nil), defaultSidecarVersionFiles...)
	if m != nil && m.VersionProbe != nil {
		file := strings.TrimSpace(m.VersionProbe.File)
		if file != "" && strings.HasSuffix(strings.ToLower(file), ".json") {
			key := strings.TrimSpace(m.VersionProbe.JSONKey)
			if key == "" {
				key = "version"
			}
			specs = append([]sidecarVersionSpec{{file: file, key: key}}, specs...)
		}
	}
	return specs
}

func detectSidecarJSONVersion(installDir string, m *manifest.Manifest) (string, bool) {
	for _, spec := range sidecarSpecs(m) {
		path := filepath.Join(installDir, filepath.FromSlash(spec.file))
		ver, err := readJSONStringField(path, spec.key)
		if err != nil || ver == "" {
			continue
		}
		return normalizeVersion(ver), true
	}
	return "", false
}

func readJSONStringField(path, key string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	field, ok := raw[key]
	if !ok {
		return "", os.ErrNotExist
	}
	var s string
	if err := json.Unmarshal(field, &s); err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// versionsCompatibleForDrift rejects comparisons across unrelated version schemes
// (e.g. Tor Browser 15.0.19 vs Firefox engine 140.13.0 in firefox.exe).
func versionsCompatibleForDrift(managed, detected string) bool {
	managed = normalizeVersion(managed)
	detected = normalizeVersion(detected)
	if managed == "" || detected == "" {
		return false
	}
	if managed == detected {
		return true
	}
	mp := parseVersionParts(managed)
	dp := parseVersionParts(detected)
	if len(mp) == 0 || len(dp) == 0 {
		return false
	}
	if mp[0].text != "" || dp[0].text != "" {
		return strings.HasPrefix(detected, managed+".") || strings.HasPrefix(managed, detected+".")
	}
	m0, d0 := mp[0].num, dp[0].num
	if m0 == d0 {
		return true
	}
	diff := m0 - d0
	if diff < 0 {
		diff = -diff
	}
	minMajor := m0
	maxMajor := d0
	if d0 < m0 {
		minMajor, maxMajor = d0, m0
	}
	// Product release (15.x) vs embedded engine/build (140.x): large gap, different scales.
	if diff > 3 && minMajor < 90 && maxMajor >= 90 {
		return false
	}
	// Same product line with adjacent majors (Chrome 120 → 121).
	return diff <= 2
}

func formatVersionParts(parts []versionPart) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('.')
		}
		if p.text != "" {
			b.WriteString(p.text)
		} else {
			b.WriteString(strconv.Itoa(p.num))
		}
	}
	return b.String()
}

// versionProbeCandidates lists executable and sidecar files to probe for a
// runtime version: manifest-declared paths first, then the Application-layout
// scans.
func versionProbeCandidates(installDir string, m *manifest.Manifest) []string {
	list := &probeCandidateList{installDir: installDir, seen: make(map[string]bool)}
	addManifestProbeCandidates(list, m)
	addScannedProbeCandidates(list)
	return list.out
}

// probeCandidateList collects sanitized, deduplicated probe paths under one
// install directory.
type probeCandidateList struct {
	installDir string
	seen       map[string]bool
	out        []string
}

// add appends one candidate path, resolving rel against the install directory.
// Absolute paths and paths that escape the install directory are ignored, as are
// duplicates.
func (l *probeCandidateList) add(rel string) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return
	}
	rel = strings.Trim(rel, `"'`)
	rel = filepath.FromSlash(rel)
	if filepath.IsAbs(rel) {
		return
	}
	abs := filepath.Clean(filepath.Join(l.installDir, rel))
	if !l.withinInstallDir(abs) {
		return
	}
	key := strings.ToLower(abs)
	if l.seen[key] {
		return
	}
	l.seen[key] = true
	l.out = append(l.out, abs)
}

// withinInstallDir reports whether abs is the install directory itself or a path
// under it (both sides cleaned, comparison case-insensitive).
func (l *probeCandidateList) withinInstallDir(abs string) bool {
	root := filepath.Clean(l.installDir)
	if strings.EqualFold(abs, root) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(abs), strings.ToLower(root)+string(os.PathSeparator))
}

// addManifestProbeCandidates adds the paths declared by the manifest (probe
// file, launch bins, bins, shortcut targets). Without a manifest, the
// install-root executables stand in.
func addManifestProbeCandidates(list *probeCandidateList, m *manifest.Manifest) {
	if m == nil {
		// Only scan install root exes when manifest metadata is unavailable.
		scanDirExecutables(list, list.installDir, "")
		return
	}
	if m.VersionProbe != nil && m.VersionProbe.File != "" {
		list.add(m.VersionProbe.File)
	}
	for _, bin := range m.LaunchBinaries() {
		list.add(binExePath(bin))
	}
	for _, bin := range m.Binaries() {
		list.add(binExePath(bin))
	}
	for _, sc := range m.LaunchShortcuts() {
		list.add(sc.Target)
	}
}

// addScannedProbeCandidates adds executables from the Chromium-style Application
// layout, including its nested version directories.
func addScannedProbeCandidates(list *probeCandidateList) {
	appDir := filepath.Join(list.installDir, "Application")
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() {
			if strings.HasSuffix(strings.ToLower(name), ".exe") {
				list.add(filepath.Join("Application", name))
			}
			continue
		}
		if looksLikeVersionDir(name) {
			scanDirExecutables(list, filepath.Join(appDir, name), filepath.Join("Application", name))
		}
	}
}

// scanDirExecutables adds the .exe files directly inside dir, addressed relative
// to the install root through relPrefix ("" for the install root itself).
func scanDirExecutables(list *probeCandidateList, dir, relPrefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".exe") {
			list.add(filepath.Join(relPrefix, e.Name()))
		}
	}
}

func binExePath(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	if strings.HasPrefix(entry, "[") && strings.HasSuffix(entry, "]") {
		inner := strings.TrimSpace(entry[1 : len(entry)-1])
		parts := strings.Split(inner, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	return entry
}

func detectFileVersion(candidates []string) (string, bool) {
	best := ""
	for _, path := range candidates {
		if !strings.HasSuffix(strings.ToLower(path), ".exe") {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		ver, err := apps.ReadFileVersion(path)
		if err != nil || ver == "" {
			continue
		}
		ver = normalizeVersion(ver)
		if best == "" || versionCompare(ver, best) > 0 {
			best = ver
		}
	}
	return best, best != ""
}

func detectCommandVersion(candidates []string, m *manifest.Manifest) (string, bool) {
	args := []string{"--version"}
	pattern := ""
	if m != nil && m.VersionProbe != nil {
		if len(m.VersionProbe.Args) > 0 {
			args = append([]string(nil), m.VersionProbe.Args...)
		}
		pattern = strings.TrimSpace(m.VersionProbe.Pattern)
	}
	var re *regexp.Regexp
	if pattern != "" {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return "", false
		}
		re = compiled
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		// Skip GUI PE binaries unless the package explicitly asked for command probing.
		explicitCommand := m != nil && m.VersionProbe != nil && strategyRequested(m.VersionProbe.Strategies, versionProbeSourceCommand)
		if !explicitCommand && isLikelyGUIExecutable(path) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), versionProbeCommandTimeout)
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Dir = filepath.Dir(path)
		procutil.HideWindow(cmd)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(out))
		if text == "" {
			continue
		}
		ver := text
		if re != nil {
			match := re.FindStringSubmatch(text)
			if len(match) < 2 {
				continue
			}
			ver = match[1]
		} else {
			ver = extractVersionToken(text)
		}
		ver = normalizeVersion(ver)
		if ver != "" {
			return ver, true
		}
	}
	return "", false
}

func strategyRequested(strategies []string, want string) bool {
	for _, s := range strategies {
		if strings.EqualFold(strings.TrimSpace(s), want) {
			return true
		}
	}
	return false
}

func isLikelyGUIExecutable(path string) bool {
	lower := strings.ToLower(filepath.Base(path))
	guiNames := []string{
		"vivaldi.exe", "chrome.exe", "msedge.exe", "brave.exe", "opera.exe",
		"code.exe", "code - insiders.exe", "firefox.exe", "slack.exe", "discord.exe",
		"spotify.exe", "telegram.exe", "wechat.exe", "qq.exe",
	}
	for _, name := range guiNames {
		if lower == name {
			return true
		}
	}
	return false
}

func extractVersionToken(text string) string {
	re := regexp.MustCompile(`(?i)v?(\d+(?:\.\d+){1,3}(?:[-_][0-9A-Za-z.]+)?)`)
	match := re.FindStringSubmatch(text)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func looksLikeVersionDir(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	parts := parseVersionParts(name)
	return len(parts) >= 2 && parts[0].text == ""
}

// detectNestedVersionDir finds Chromium-style Application/<version>/ folders newer
// than what FileVersion could resolve (folder name itself is the version signal).
func detectNestedVersionDir(installDir string) (string, bool) {
	appDir := filepath.Join(installDir, "Application")
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return "", false
	}
	best := ""
	for _, e := range entries {
		if !e.IsDir() || !looksLikeVersionDir(e.Name()) {
			continue
		}
		ver := normalizeVersion(e.Name())
		if best == "" || versionCompare(ver, best) > 0 {
			best = ver
		}
	}
	return best, best != ""
}

func (e *Engine) detectPackageVersion(name, version string) VersionDetectResult {
	if e == nil || e.Config == nil {
		return VersionDetectResult{ManagedVersion: version}
	}
	installDir := filepath.Join(apps.PkgRoot(e.Config.RootDir, name), version)
	var m *manifest.Manifest
	if rec, err := apps.LoadInstallRecord(installDir); err == nil && rec != nil {
		m = rec.Manifest
	}
	return DetectInstalledVersion(installDir, version, m)
}
