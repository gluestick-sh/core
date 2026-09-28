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

## [0.1.0] - 2026-07-02

### Added

- Initial embeddable `engine` API: install, uninstall, search, catalog, progress reporters.
- Scoop-compatible manifest parsing, CAS store, SQLite cache index, and bucket registry.
- Typed errors (`apperr`) and manifest-relative path validation (`safepath`) with traversal/zip-slip protection.

[0.1.0]: https://github.com/gluestick-sh/core/releases/tag/v0.1.0
