# Changelog

All notable changes to **core** are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added

- `shim` now owns bin-entry parsing (`ParseBinPattern`, `ParseBinPatternParts`) and
  bin candidate resolution (`ResolveBinCandidates`, `InstallRootBinCandidates`,
  `ShouldSkipPortableScanDir`), moved out of the internal install package so
  `launch` no longer imports `install`.
- First tests for `envset`, covering the empty-input short circuit that returns
  before the platform layer touches the user environment.

### Changed

- Internal package layout: `engine/internal/lifecycle` now owns package lifecycle
  side effects and their teardown — hook scripts, 7-Zip/dark resolution, persist
  handling, start-menu shortcuts, and file removal — so `uninstall` no longer
  imports `install` and the engine facade performs the post-uninstall version
  relink. Progress marks moved to `verbose`; shim meta helpers and shim removal
  moved to `shim`. No public API change for consumers.
- Install timing instrumentation is now named "timings" end to end: the request option
  is `Options["timings"]`, the engine prints one `GLUE_TIMINGS` line per package with
  per-phase millisecond fields, and the internal symbols and file follow
  (`installPhaseTimings`, `timingsEnabled`, `parseInstallTimingsLine`,
  `install_timings.go`).
- `bootstrap` is split from a single 736-line file into `download`, `fsutil`,
  `git`, `sevenzip`, and `tools`; WiX dark and innounp now share one `zipTool`
  install path.
- `snapshot.Diff` is split into single-purpose helpers (`indexLocalState`,
  `planBucketAdds`, `planPackageInstalls`, `planPackageActivations`,
  `planRemovals`, `sortPlan`) so the diff reads as one pass of plan steps
  instead of one deeply nested function; behaviour is unchanged.
- `manifest.binariesFromField` is split into per-shape helpers
  (`binEntriesFromArray`, `binEntryFromElement`, `binEntryFromPair`,
  `binEntryFromObject`, `binPattern`) so bin normalization no longer nests four
  levels deep; behaviour is unchanged.
- `manifest.parsePersistEntries` is split the same way
  (`persistEntriesFromArray`, `persistEntryFromPath`, `persistEntryFromPair`) so
  persist `[install, data]` pairs no longer nest three levels deep; behaviour is
  unchanged.
- `extractor.extractWith7z` and `ExtractEXE` are split into single-purpose helpers
  (`extractStages`, `cacheAndExtractNestedTar`, `newExtractTempDir`,
  `sevenZipBinary`, `sevenZipCanList`, `extractSFXArchive`,
  `copyExecutableToTemp`, `storeSingleExecutable`). The nested tar scan now reuses
  `findNestedTar` and `ExtractEXE` no longer builds a command it immediately
  discards; behaviour is unchanged apart from the SFX error fix below.
- `shim.cacheShimStub` stages the runner beside its destination and renames it into
  place (retrying once when Windows keeps the old file locked), so an interrupted
  copy can no longer leave a truncated `shim.exe` in the glue root. The candidate
  scan is extracted into `firstExistingPath` and `executableDir`.
- `message.FormatEN` no longer formats through a ~100-case switch. The English
  fallbacks are data tables (`enMessages`, `enMessageVariants`) rendered from
  `{name}`/`{#name}` placeholders, and the old default branch (which returned the
  key either way) collapses to a single return. Output was verified byte-identical
  against the previous switch for every key and arg shape.
- `install.finalizePackageInstall` is split into per-phase `finalizeState` methods
  (persist restore, pre_install, installer script, flatten/validate, post_install,
  link/record, shims/shortcuts, cache index), so the finalize pipeline reads as the
  numbered phase list in its doc comment; call order is unchanged.
- `install.resolveInstallPhase` is split into `installState` methods matching its
  numbered steps (`loadManifest`, `resolvePinnedVersion`, `checkExistingInstall`
  with the version-lock guard and the reuse/activate paths, `describePlan`,
  `applyManifestOverrides`); the done paths still return before the plan display,
  as before.
- `uninstall.PackageFull` is split into an `uninstallJob` with phase methods
  (`cleanupLeftoverInstall`, `resolveUninstallVersion`, `runUninstallHooksAndSavePersist`,
  `removeCurrentAndVersionDir` with the failed-removal recovery, `finishAfterRemoval`
  with the last-version cleanup); step order and error wording are unchanged.
- `install.createPackageShims` is split into a `shimCreator` (conflict skip and
  first-error recording) plus per-source helpers (`createManifestBinShims` with
  `binRelPath`, `createEnvAddPathShims` with the directory scan); the discarded
  `exePath` assignment is gone and the "_" mirror fallback is unchanged.

### Removed

- Dead `snapshot` source constants `SourceProfile` and `SourceWorkspace`: no code ever
  wrote or read them.
- Dead unexported helpers: `verbose.successMark` / `verbose.failedMark` (superseded by
  the exported marks) and `lifecycle.substituteInstallHookVars` (superseded by
  `BuildInstallHookScript` variable injection).

### Fixed

- Stripped UTF-8 BOMs from nine `engine/internal/install` sources and the trailing
  blank lines in `install_hooks_post.go`. Go tolerated them, but they shifted line
  numbers and fought `gofmt`.
- SFX extraction failures now report the real 7z error. `ExtractEXE` formatted the
  failure with a leftover `nil` variable (`7z extract SFX failed: %!w(<nil>)`),
  which hid the child process exit error and its stderr context.

## [0.1.0] - 2026-07-02

### Added

- Initial embeddable `engine` API: install, uninstall, search, catalog, progress reporters.
- Scoop-compatible manifest parsing, CAS store, SQLite cache index, and bucket registry.
- Typed errors (`apperr`) and manifest-relative path validation (`safepath`) with traversal/zip-slip protection.

[0.1.0]: https://github.com/gluestick-sh/core/releases/tag/v0.1.0
