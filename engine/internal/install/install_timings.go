// Install phase timings for `glue install --timings`.
//
// When etypes.InstallRequest.Options["timings"] is "true", installPackageFull wraps major
// steps with installPhaseTimings timers and prints one machine-readable line:
//
//	GLUE_TIMINGS download_ms=... cas_ms=... extract_ms=... link_ms=... shim_ms=... cache_ms=... bootstrap_ms=... total_ms=...
//
// cas_ms is the legacy field name for content-store ingest time (store ingest).
//
// External benchmark scripts parse that line by field order; parseInstallTimingsLine
// supports the same format in tests.
package install

import (
	"regexp"
	"strconv"
	"time"

	"github.com/gluestick-sh/core/downloader"
	"github.com/gluestick-sh/core/verbose"
)

// installOptionTimings is the InstallRequest.Options key that enables per-phase
// install timing accumulation and the GLUE_TIMINGS summary line.
const installOptionTimings = "timings"

// installPhaseTimings accumulates wall-clock time per install phase for one package.
// All methods are nil-safe: when timings are not requested the struct is nil and
// wrappers become no-ops.
type installPhaseTimings struct {
	enabled bool // set from installOptionTimings

	Download    time.Duration // HTTP/FTP network time (from downloader.Result.Timing)
	StoreIngest time.Duration // writing downloaded blobs into the content store
	Extract     time.Duration // archive/script extraction into content store or install dir
	Link        time.Duration // hardlinking content store objects into apps/<pkg>/<ver>
	Shim        time.Duration // shim.exe + metadata creation
	Cache       time.Duration // SQLite cache index update
	Bootstrap   time.Duration // on-demand git / 7z / dark / innounp setup
}

// absorbDownloadResults adds per-artifact network and content store ingest times after a download batch.
func (p *installPhaseTimings) absorbDownloadResults(results []downloader.Result) {
	if p == nil {
		return
	}
	for _, r := range results {
		p.Download += r.Timing.Network
		p.StoreIngest += r.Timing.StoreIngest
	}
}

// runBootstrap times bootstrap helper setup (git, 7z, dark, innounp).
func (p *installPhaseTimings) runBootstrap(fn func() error) error {
	if p == nil {
		return fn()
	}
	start := time.Now()
	err := fn()
	p.Bootstrap += time.Since(start)
	return err
}

// runExtract times archive or installer-script extraction (7z, innounp, etc.).
func (p *installPhaseTimings) runExtract(fn func() error) error {
	start := time.Now()
	err := fn()
	if p != nil {
		p.Extract += time.Since(start)
	}
	return err
}

// addLink records elapsed time since linkStart for a store→apps hardlink batch.
func (p *installPhaseTimings) addLink(start time.Time) {
	if p == nil {
		return
	}
	p.Link += time.Since(start)
}

// runShim times shim removal/recreation at the end of install.
func (p *installPhaseTimings) runShim(fn func() error) error {
	if p == nil {
		return fn()
	}
	start := time.Now()
	err := fn()
	p.Shim += time.Since(start)
	return err
}

// runCache times the post-install cache index write.
func (p *installPhaseTimings) runCache(fn func() error) error {
	if p == nil {
		return fn()
	}
	start := time.Now()
	err := fn()
	p.Cache += time.Since(start)
	return err
}

// total returns the sum of phase buckets (not necessarily equal to end-to-end wall time).
func (p *installPhaseTimings) total() time.Duration {
	if p == nil {
		return 0
	}
	return p.Download + p.StoreIngest + p.Extract + p.Link + p.Shim + p.Cache + p.Bootstrap
}

// emit prints the GLUE_TIMINGS summary when timings were requested for this install.
func (p *installPhaseTimings) emit() {
	if p == nil || !p.enabled {
		return
	}
	verbose.Progressf(
		"GLUE_TIMINGS download_ms=%d cas_ms=%d extract_ms=%d link_ms=%d shim_ms=%d cache_ms=%d bootstrap_ms=%d total_ms=%d\n",
		p.Download.Milliseconds(),
		p.StoreIngest.Milliseconds(),
		p.Extract.Milliseconds(),
		p.Link.Milliseconds(),
		p.Shim.Milliseconds(),
		p.Cache.Milliseconds(),
		p.Bootstrap.Milliseconds(),
		p.total().Milliseconds(),
	)
}

// glueTimingsLine matches the GLUE_TIMINGS line emitted by emit (field order is stable).
var glueTimingsLine = regexp.MustCompile(
	`^GLUE_TIMINGS download_ms=(\d+) cas_ms=(\d+) extract_ms=(\d+) link_ms=(\d+) shim_ms=(\d+) cache_ms=(\d+) bootstrap_ms=(\d+) total_ms=(\d+)$`,
)

// parsedInstallTimings holds millisecond values from a GLUE_TIMINGS line.
type parsedInstallTimings struct {
	DownloadMs  int64
	StoreMs     int64
	ExtractMs   int64
	LinkMs      int64
	ShimMs      int64
	CacheMs     int64
	BootstrapMs int64
	TotalMs     int64
}

// parseInstallTimingsLine parses a GLUE_TIMINGS log line (used by tests and benchmarks).
func parseInstallTimingsLine(line string) (parsedInstallTimings, bool) {
	m := glueTimingsLine.FindStringSubmatch(line)
	if m == nil {
		return parsedInstallTimings{}, false
	}
	vals := make([]int64, 8)
	for i := 0; i < 8; i++ {
		n, err := strconv.ParseInt(m[i+1], 10, 64)
		if err != nil {
			return parsedInstallTimings{}, false
		}
		vals[i] = n
	}
	return parsedInstallTimings{
		DownloadMs:  vals[0],
		StoreMs:     vals[1],
		ExtractMs:   vals[2],
		LinkMs:      vals[3],
		ShimMs:      vals[4],
		CacheMs:     vals[5],
		BootstrapMs: vals[6],
		TotalMs:     vals[7],
	}, true
}
