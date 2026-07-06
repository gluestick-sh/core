package engine

import (
	"github.com/gluestick-sh/core/engine/internal/override"
	"github.com/gluestick-sh/core/engine/internal/runtime"

	"strings"
	"testing"

	"github.com/gluestick-sh/core/config"
	"github.com/gluestick-sh/core/manifest"
)

func TestApplyManifestDownloadOverrides(t *testing.T) {
	root := t.TempDir()
	m, err := manifest.Parse(strings.NewReader(`{
		"version": "3.5.1",
		"url": "https://example.com/old.exe",
		"hash": "abc"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Engine: &runtime.Engine{Config: &EngineConfig{RootDir: root}}}
	if err := e.SetManifestDownloadOverride("lemon/1key.run", []string{"https://example.com/new.exe"}, nil); err != nil {
		t.Fatal(err)
	}
	out, err := override.ApplyManifestOverrides(e.Engine, "lemon/1key.run", "", m, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.GetURL(); got != "https://example.com/new.exe" {
		t.Fatalf("url = %q", got)
	}
}

func TestManifestDownloadOverrideConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := config.SetConfigManifestDownloadOverride(root, "Lemon/1key.run", []string{"https://example.com/x.exe"}, []string{"deadbeef"}); err != nil {
		t.Fatal(err)
	}
	got, err := config.ReadConfigManifestDownloadOverrides(root)
	if err != nil {
		t.Fatal(err)
	}
	item, ok := got["lemon/1key.run"]
	if !ok || item.URLs[0] != "https://example.com/x.exe" {
		t.Fatalf("override = %#v, ok=%v", item, ok)
	}
}
