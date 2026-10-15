package message

import (
	"strings"
	"testing"
)

// knownKeys must always resolve to human-readable English (not the raw key).
var knownKeys = []string{
	ProgressInstallStarting,
	ProgressInstallComplete,
	ProgressInstallCancelled,
	ProgressUninstallStarting,
	ProgressUninstallComplete,
	ProgressResolvingManifest,
	ProgressPreparingDownload,
	ProgressDownloading,
	ProgressDownloadCached,
	ProgressLinkingFiles,
	ProgressCreatingShims,
	ProgressUpdatingCache,
	ProgressExtracting,
	ProgressPackageInstallComplete,
	GCPrepareStore,
	GCReadingIndexRefs,
	BucketCloning,
	BucketUpdateComplete,
	BucketNoUpdates,
	GitPulling,
	DoctorGitMissing,
	DoctorHintGitInstall,
	ErrLaunchNotOpenable,
}

func TestFormatENKnownKeys(t *testing.T) {
	for _, key := range knownKeys {
		got := FormatEN(key, nil)
		if got == "" || got == key {
			t.Fatalf("FormatEN(%q) = %q, want readable English", key, got)
		}
		if strings.HasPrefix(got, "progress.") || strings.HasPrefix(got, "doctor.") || strings.HasPrefix(got, "error.") {
			t.Fatalf("FormatEN(%q) returned untranslated key: %q", key, got)
		}
	}
}

func TestFormatENWithArgs(t *testing.T) {
	got := FormatEN(ProgressPackageInstallComplete, map[string]any{"package": "git"})
	if !strings.Contains(got, "git") {
		t.Fatalf("expected package name in message, got %q", got)
	}
	got = FormatEN(ProgressDownloading, map[string]interface{}{"file": "node-v20.zip"})
	if !strings.Contains(got, "node-v20.zip") {
		t.Fatalf("expected filename in download message, got %q", got)
	}
}

// templateArgs fills every placeholder in tpl: int args with 7, string args with "pkg".
func templateArgs(tpl string) map[string]any {
	args := map[string]any{}
	for _, ph := range placeholderPattern.FindAllString(tpl, -1) {
		if strings.HasPrefix(ph, "{#") {
			args[strings.Trim(ph, "{#}")] = 7
		} else {
			args[strings.Trim(ph, "{}")] = "pkg"
		}
	}
	return args
}

func TestFormatENRendersEveryEntry(t *testing.T) {
	for key, tpl := range enMessages {
		if got := FormatEN(key, templateArgs(tpl)); strings.ContainsAny(got, "{}") {
			t.Errorf("FormatEN(%q) = %q: unresolved placeholder", key, got)
		}
	}
	for key, variants := range enMessageVariants {
		args := map[string]any{}
		for _, v := range variants {
			for name, val := range templateArgs(v.tpl) {
				args[name] = val
			}
			for _, rule := range v.require {
				name := strings.TrimPrefix(strings.TrimPrefix(rule, "!"), "#")
				if _, ok := args[name]; !ok {
					if strings.Contains(rule, "#") {
						args[name] = 7
					} else {
						args[name] = "pkg"
					}
				}
			}
		}
		if got := FormatEN(key, args); strings.ContainsAny(got, "{}") {
			t.Errorf("FormatEN(%q) = %q: unresolved placeholder", key, got)
		}
	}
}

func TestFormatENUnknownKeyReturnsKey(t *testing.T) {
	for _, key := range []string{"progress.unknown", "error.unknown", "doctor.unknown", "plain-key", ""} {
		if got := FormatEN(key, map[string]any{"x": "y"}); got != key {
			t.Errorf("FormatEN(%q) = %q, want the key echoed", key, got)
		}
	}
}

func TestFormatENIntCoercionAndVariantFallbacks(t *testing.T) {
	tests := []struct {
		name string
		key  string
		args map[string]any
		want string
	}{
		{"int64 percent renders", ProgressExtractDecompress, map[string]any{"percent": int64(45)}, "Decompressing archive (45%)"},
		{"float percent renders", ProgressExtractDecompress, map[string]any{"percent": float64(45)}, "Decompressing archive (45%)"},
		{"string percent falls back", ProgressExtractDecompress, map[string]any{"percent": "45"}, "Decompressing archive..."},
		{"zero percent falls back", ProgressExtractDecompress, map[string]any{"percent": 0}, "Decompressing archive..."},
		{"purge prepare wording", PurgeDeletingFile, map[string]any{"total": 9, "current": 0}, "Preparing to delete unreferenced cache files (9 total)..."},
		{"purge progress wording", PurgeDeletingFile, map[string]any{"total": 9, "current": 1}, "Deleting unreferenced cache files (1/9)..."},
		{"store scan with total", GCScanningStore, map[string]any{"scanned": 5, "current": 2, "total": 8}, "Scanning store objects… checked 5 (2/8)"},
		{"store scan without total", GCScanningStore, map[string]any{"scanned": 5}, "Scanning store objects… checked 5"},
		{"missing kind falls back", ErrLaunchInvalidKind, nil, "Invalid launch mode"},
	}
	for _, tc := range tests {
		if got := FormatEN(tc.key, tc.args); got != tc.want {
			t.Errorf("%s: FormatEN(%q, %#v) = %q, want %q", tc.name, tc.key, tc.args, got, tc.want)
		}
	}
}
