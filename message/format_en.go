package message

import (
	"regexp"
	"strconv"
	"strings"
)

// FormatEN returns an English fallback string for CLI and logs.
func FormatEN(key string, args map[string]any) string {
	if tpl, ok := enMessages[key]; ok {
		return expandTemplate(tpl, args)
	}
	for _, v := range enMessageVariants[key] {
		if msg, ok := v.render(args); ok {
			return msg
		}
	}
	return key
}

// enVariant is one wording rule for messages whose text depends on which args are
// present; the first variant whose requirements hold wins.
type enVariant struct {
	tpl     string   // "{name}" marks a string arg, "{#name}" an int arg
	require []string // "#name" needs a positive int, "name" a non-empty string; "!" negates
}

func (v enVariant) render(args map[string]any) (string, bool) {
	for _, rule := range v.require {
		if !argPresent(args, rule) {
			return "", false
		}
	}
	return expandTemplate(v.tpl, args), true
}

// argPresent reports whether args satisfies one require rule.
func argPresent(args map[string]any, rule string) bool {
	want := !strings.HasPrefix(rule, "!")
	name := strings.TrimPrefix(rule, "!")
	if strings.HasPrefix(name, "#") {
		return (intArg(args, name[1:]) > 0) == want
	}
	return (strArg(args, name) != "") == want
}

// strArg returns args[name] as a string, or "" when absent or another type.
func strArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	v, _ := args[name].(string)
	return v
}

// intArg coerces args[name] (int, int64, float64) to int, or 0.
func intArg(args map[string]any, name string) int {
	if args == nil {
		return 0
	}
	switch v := args[name].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

var placeholderPattern = regexp.MustCompile(`\{#?([a-zA-Z_]+)\}`)

// expandTemplate fills "{name}" and "{#name}" placeholders from args.
func expandTemplate(tpl string, args map[string]any) string {
	return placeholderPattern.ReplaceAllStringFunc(tpl, func(m string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(m, "{"), "}")
		if strings.HasPrefix(name, "#") {
			return strconv.Itoa(intArg(args, name[1:]))
		}
		return strArg(args, name)
	})
}

// enMessageVariants holds the keys whose wording depends on args; every list ends
// with an unconditional fallback variant.
var enMessageVariants = map[string][]enVariant{
	ProgressPreparingDownload: {
		{tpl: "Preparing to download {file}...", require: []string{"file"}},
		{tpl: "Preparing download..."},
	},
	ProgressDownloading: {
		{tpl: "Downloading {file}...", require: []string{"file"}},
		{tpl: "Downloading..."},
	},
	ProgressDownloadComplete: {
		{tpl: "Downloaded {size}", require: []string{"size"}},
		{tpl: "Download complete"},
	},
	ProgressExtractDecompress: {
		{tpl: "Decompressing archive ({#percent}%)", require: []string{"#percent"}},
		{tpl: "Decompressing archive..."},
	},
	ProgressExtractIndexing: {
		{tpl: "Indexing installed files ({#current}/{#total})", require: []string{"#total"}},
		{tpl: "Indexing installed files ({#current})"},
	},
	ProgressPackageInstallComplete: {
		{tpl: "{package} installed", require: []string{"package"}},
		{tpl: "Package installed"},
	},
	GCScanAppsEmpty: {
		{tpl: "Installed apps directory {path} is empty; skipping scan", require: []string{"path"}},
		{tpl: "No installed apps to scan"},
	},
	GCScanningStore: {
		{tpl: "Scanning store objects… checked {#scanned} ({#current}/{#total})", require: []string{"#total"}},
		{tpl: "Scanning store objects… checked {#scanned}"},
	},
	GCNoOrphans: {
		{tpl: "Nothing to clean: no orphan blobs in {path}", require: []string{"path"}},
		{tpl: "Nothing to clean"},
	},
	PurgeDeletingFile: {
		{tpl: "Preparing to delete unreferenced cache files ({#total} total)...", require: []string{"#total", "!#current"}},
		{tpl: "Deleting unreferenced cache files ({#current}/{#total})..."},
	},
	ErrLaunchInvalidKind: {
		{tpl: "Invalid launch mode: {kind}", require: []string{"kind"}},
		{tpl: "Invalid launch mode"},
	},
}

// enMessages maps message keys to English fallbacks. "{name}" marks a string arg
// and "{#name}" an int arg; keys with arg-dependent wording live in
// enMessageVariants instead.
var enMessages = map[string]string{
	ProgressInstallStarting:   "Starting install...",
	ProgressInstallComplete:   "Install complete",
	ProgressInstallCancelled:  "Install cancelled",
	ProgressUninstallStarting: "Uninstalling...",
	ProgressUninstallComplete: "Uninstall complete",
	ProgressResolvingManifest: "Resolving package manifest...",
	ProgressDownloadCached:    "Using cached download",
	ProgressLinkingFiles:      "Linking files...",
	ProgressCreatingShims:     "Creating shims...",
	ProgressUpdatingCache:     "Updating cache index...",
	ProgressExtracting:        "Extracting and installing files...",
	ProgressExtractProcessing: "Processing files ({#current}/{#total})",
	GCPrepareStore:            "Preparing cache cleanup...",
	GCClearingPartials:        "Clearing incomplete downloads...",
	GCClearingSidecars:        "Clearing stale download indexes...",
	GCReadingIndexRefs:        "Reading file references from cache index...",
	GCIndexRefCount:           "Cache index has {#count} file references",
	GCScanAppsStart:           "Scanning installed apps...",
	GCScanAppsSkipped:         "Installed apps directory not found; skipping app scan",
	GCAppsPendingScan:         "{#count} installed packages to scan",
	GCBuildingStoreIndex:      "Indexing store hardlinks… {#indexed} objects ({#current}/{#total})",
	GCAppsEnumeratingDirs:     "Planning scan for {#packages} installed packages…",
	GCAppsScanPlan:            "Will scan {#fileTotal} files in {#dirTotal} directories across {#packages} packages",
	GCScanningApp:             "Scanning {package} ({#current}/{#total})",
	GCScanningAppsMerged:      "Scanning installed apps: {#current}/{#total} packages, {#dirsDone}/{#dirTotal} directories, {#files}/{#fileTotal} files",
	GCAppsScanComplete:        "App scan complete; {#count} file references",
	GCRefsCollected:           "Collected {#count} valid file references",
	GCScanStoreStart:          "Scanning store objects...",
	GCScanStoreComplete:       "Store scan complete: checked {#scanned} objects, found {#orphans} orphan blobs",
	GCOrphansFound:            "Found {#count} orphan blobs; deleting...",
	GCDeletingOrphan:          "Deleting orphan {label} ({#current}/{#total}, {size})",
	GCDeletingOrphansBatch:    "Deleting orphan blobs ({#current}/{#total})",
	GCCompleteFreed:           "Removed {#removed} junk items, freed {freed}",
	GCCompleteNothing:         "No space to reclaim",

	PurgePrepare:               "Preparing to remove {name} from cache index...",
	PurgeRemovingIndex:         "Removing {name} from cache index ({#files} files)...",
	PurgeIndexRemoved:          "Removed {name} from cache index; checking file references...",
	PurgeScanningFiles:         "Scanning installed files for {package} ({#scanned} files checked)...",
	PurgeScanningInstall:       "Checking whether {name} still uses these cache files...",
	PurgeScanningInstalls:      "Checking whether {name} or {#count} related packages still use these cache files...",
	PurgeScanningOtherInstalls: "Checking other installed packages for remaining cache references ({#count} packages)...",
	PurgeNoInstallScan:         "{name} is not installed; skipping install directory scan",
	PurgeCheckingRefs:          "Checking remaining cache references...",
	PurgeCompleteFreed:         "Removed {#removed} cache blobs, freed {freed}",
	PurgeCompleteNothing:       "No unreferenced cache files to delete",
	BucketCloning:              "Cloning {name}...",
	BucketUpdating:             "Updating {name} ({#current}/{#total})...",
	BucketUpdateComplete:       "Bucket update complete",
	BucketPrepareAdd:           "Preparing to add bucket...",
	BucketIndexRefresh:         "Refreshing package index...",
	BucketRemoving:             "Removing bucket...",
	BucketRemoveComplete:       "Bucket removed",
	BucketNoUpdates:            "No buckets need updating",
	GitPulling:                 "Pulling updates...",

	BootstrapInnounpDetecting:   "Checking for innounp…",
	BootstrapInnounpDiscovering: "Scanning catalog for Inno Setup packages…",
	BootstrapInnounpDownloading: "Downloading innounp…",
	BootstrapInnounpExtracting:  "Extracting innounp…",
	BootstrapInnounpComplete:    "innounp is ready",

	BootstrapGitDetecting:   "Checking for Git…",
	BootstrapGitDownloading: "Downloading MinGit…",
	BootstrapGitExtracting:  "Extracting MinGit…",
	BootstrapGitComplete:    "Git is ready",

	BootstrapSevenZipDetecting:   "Checking for 7-Zip…",
	BootstrapSevenZipDownloading: "Downloading 7-Zip…",
	BootstrapSevenZipExtracting:  "Extracting 7-Zip…",
	BootstrapSevenZipComplete:    "7-Zip is ready",

	BootstrapWixDetecting:       "Checking for WiX toolset…",
	BootstrapWixDiscovering:     "Scanning catalog for WiX Burn packages…",
	BootstrapWixDownloading:     "Downloading WiX toolset…",
	BootstrapWixExtracting:      "Extracting WiX toolset…",
	BootstrapWixComplete:        "WiX toolset is ready",
	DoctorGlueRootUnknown:       "Cannot determine ~/.glue path",
	DoctorGlueRootNotWritable:   "Glue data directory is not writable",
	DoctorGitInPath:             "Git is available on PATH",
	DoctorGitMissing:            "Git was not found",
	DoctorGitWillBootstrap:      "Git not installed yet; downloads automatically on first bucket add",
	DoctorSevenZipMissing:       "7-Zip was not found",
	DoctorSevenZipWillBootstrap: "7-Zip not installed yet; downloads automatically on first archive install",
	DoctorDarkMissing:           "WiX dark was not found",
	DoctorDarkWillBootstrap:     "WiX not installed yet; downloads automatically when a Burn package needs it",
	DoctorInnounpMissing:        "innounp was not found",
	DoctorInnounpWillBootstrap:  "innounp not installed yet; downloads automatically when an Inno Setup package needs it",
	DoctorShimInPath:            "Shim directory is on PATH",
	DoctorShimNotInPath:         "Shim directory is not on PATH",
	DoctorGitHubDirectOK:        "Direct connection to GitHub is available",
	DoctorGitHubMirrorOK:        "GitHub mirror is available",
	DoctorGitHubGitOK:           "Git access to GitHub repositories works (bucket updates)",
	DoctorGitHubDirectFailed:    "HTTPS access to github.com failed (package downloads may need a mirror)",
	DoctorGitHubAllFailed:       "Git, HTTPS, and configured mirrors are all unavailable",
	DoctorHintGlueRootAccess:    "Check directory permissions or run as administrator",
	DoctorHintGitInstall:        "Bucket management requires Git; it is installed automatically on first bucket add, or install Git manually",
	DoctorHintSevenZip:          "Most archive installs need 7-Zip; it is installed automatically on first extract, or run: glue install 7zip",
	DoctorHintDark:              "WiX Burn packages need dark.exe; it is installed automatically on first use, or run: glue install dark",
	DoctorHintInnounp:           "Inno Setup packages need innounp; it is installed automatically on first use, or run: glue install innounp",
	DoctorHintShimPath:          "Run glue path to see how to add the shim directory to PATH",
	DoctorHintGitHubProxy:       "Configure a GitHub mirror for package downloads; bucket updates use Git to reach GitHub",
	DoctorHintGitHubMirror:      "Verify mirror URLs or try a different mirror",
	DoctorStartupGitNote:        "git not available, will be bootstrapped when needed",
	DoctorStartupSevenZipNote:   "7z not available, will be bootstrapped when needed",

	ErrInvalidLaunchPath:      "Invalid executable path",
	ErrLaunchUnsupportedType:  "Only exe, bat, cmd, and jar files are supported",
	ErrLaunchFileMissing:      "File not found",
	ErrLaunchNotOpenable:      "This program cannot be opened directly",
	ErrLaunchInvalidRelPath:   "Invalid path",
	ErrUninstallProcessesOpen: "Cannot uninstall {package}@{version}: processes still running",
}
