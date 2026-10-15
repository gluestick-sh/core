package manifest

import (
	"strings"
	"testing"
)

func TestPersistEntries_ultravnc(t *testing.T) {
	raw := `{
		"version": "1.6.4.0",
		"url": "https://example.com/ultravnc.zip",
		"hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"persist": ["UltraVnc.ini", "options.vnc"],
		"pre_install": [
			"if (!(Test-Path \"$persist_dir\\UltraVnc.ini\")) { New-Item \"$dir\\UltraVnc.ini\" | Out-Null }"
		]
	}`
	m, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	entries := m.PersistEntries()
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].InstallName() != "UltraVnc.ini" || entries[1].InstallName() != "options.vnc" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestPersistEntry_LooksLikeFile(t *testing.T) {
	file := PersistEntry{Install: "nativeLang.xml", Data: "nativeLang.xml"}
	dir := PersistEntry{Install: "plugins", Data: "plugins"}
	profile := PersistEntry{Install: `TorBrowser\Data\Browser\profile.default`, Data: `TorBrowser\Data\Browser\profile.default`}
	if !file.LooksLikeFile() {
		t.Fatal("xml should look like file")
	}
	if dir.LooksLikeFile() {
		t.Fatal("plugins should look like directory")
	}
	if profile.LooksLikeFile() {
		t.Fatal("profile.default should look like directory")
	}
}

func TestPersistEntries_renamed(t *testing.T) {
	raw := `{"version":"1.0","url":"https://example.com/app.zip","hash":"sha256:0000000000000000000000000000000000000000000000000000000000000000","persist": [["config.ini", "app.ini"]]}`
	m, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	entries := m.PersistEntries()
	if len(entries) != 1 || entries[0].InstallName() != "config.ini" || entries[0].DataName() != "app.ini" {
		t.Fatalf("entries = %#v", entries)
	}
}
func TestPersistEntries_stringForm(t *testing.T) {
	m, err := Parse(strings.NewReader(`{"version":"1.0","url":"https://example.com/app.zip","hash":"h","persist":"config.ini"}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	entries := m.PersistEntries()
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].InstallName() != "config.ini" || entries[0].DataName() != "config.ini" {
		t.Fatalf("entry = %#v", entries[0])
	}
}

func TestPersistEntries_scalarForms(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
		want []PersistEntry
	}{
		{"nil", nil, nil},
		{"empty string", "", nil},
		{"single string", `config\app.ini`, []PersistEntry{{Install: `config\app.ini`, Data: `config\app.ini`}}},
		{"empty array", []interface{}{}, nil},
		{"unsupported type", 42, nil},
	}
	for _, tc := range tests {
		got := parsePersistEntries(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: parsePersistEntries(%#v) = %#v, want %#v", tc.name, tc.raw, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("%s: entry[%d] = %#v, want %#v", tc.name, i, got[i], tc.want[i])
			}
		}
	}
}

func TestPersistEntries_arrayVariants(t *testing.T) {
	raw := []interface{}{
		"plain.ini",
		"",
		[]interface{}{"config.ini", "app.ini"},
		[]interface{}{"solo.dir"},
		[]interface{}{"kept.ini", ""},
		[]interface{}{"kept.ini", 42},
		[]interface{}{},
		[]interface{}{42, "ignored.ini"},
		[]interface{}{nil},
		[]interface{}{"extra.ini", "data.ini", "ignored"},
		42,
		true,
	}
	got := parsePersistEntries(raw)
	want := []PersistEntry{
		{Install: "plain.ini", Data: "plain.ini"},
		{Install: "config.ini", Data: "app.ini"},
		{Install: "solo.dir", Data: "solo.dir"},
		{Install: "kept.ini", Data: "kept.ini"},
		{Install: "kept.ini", Data: "kept.ini"},
		{Install: "extra.ini", Data: "data.ini"},
	}
	if len(got) != len(want) {
		t.Fatalf("parsePersistEntries = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
