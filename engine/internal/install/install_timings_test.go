package install

import (
	"testing"
	"time"
)

func TestParseInstallTimingsLine(t *testing.T) {
	line := "GLUE_TIMINGS download_ms=1200 cas_ms=300 extract_ms=450 link_ms=0 shim_ms=12 cache_ms=3 bootstrap_ms=0 total_ms=1965"
	got, ok := parseInstallTimingsLine(line)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if got.DownloadMs != 1200 || got.StoreMs != 300 || got.ExtractMs != 450 || got.TotalMs != 1965 {
		t.Fatalf("unexpected values: %+v", got)
	}
	if _, ok := parseInstallTimingsLine("not a timings line"); ok {
		t.Fatal("expected reject")
	}
}

func TestInstallTimingsTotal(t *testing.T) {
	p := &installPhaseTimings{
		Download:    timeMs(100),
		StoreIngest: timeMs(50),
		Extract:     timeMs(200),
		Link:        timeMs(10),
		Shim:        timeMs(5),
		Cache:       timeMs(2),
		Bootstrap:   timeMs(3),
	}
	if p.total().Milliseconds() != 370 {
		t.Fatalf("total=%d", p.total().Milliseconds())
	}
}

func timeMs(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
