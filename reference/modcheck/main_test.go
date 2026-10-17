package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckTag(t *testing.T) {
	const mod = "github.com/dimchansky/fpgrowth-go"
	valid := []string{
		"v0.1.0", "v1.0.0", "v0.0.0", "v1.20.30",
		"v1.2.3-rc.1", "v1.2.3-alpha", "v1.2.3-alpha.beta", "v1.2.3-0",
		"v1.2.3-0a", "v1.2.3-x-y-z.--", "v1.2.3-rc.0", "v1.2.3-01a",
		"v1.0.0-alpha.1.2.3",
	}
	invalid := []string{
		"", "1.2.3", "v1", "v1.2", "v1.2.3.4", "V1.2.3", "v01.2.3", "v1.02.3",
		"v1.2.03", "v1.2.3-", "v1.2.3-rc..1", "v1.2.3-.rc", "v1.2.3-rc.",
		"v1.2.3-rc.01", "v1.2.3-00", "v1.2.3+build", "v1.2.3-rc.1+build",
		"v1.2.3-rc_1", "v1.2.3 ", " v1.2.3", "v1.2.3\n", "v-1.2.3",
		"v2.0.0", // needs /v2
	}
	for _, tag := range valid {
		if err := checkTag(tag, mod); err != nil {
			t.Errorf("checkTag(%q) = %v, want nil", tag, err)
		}
	}
	for _, tag := range invalid {
		if err := checkTag(tag, mod); err == nil {
			t.Errorf("checkTag(%q) = nil, want error", tag)
		}
	}
	if err := checkTag("v2.1.0-rc.1", mod+"/v2"); err != nil {
		t.Errorf("v2 tag for /v2 module: %v", err)
	}
	for _, tag := range []string{"v1.0.0", "v3.0.0"} {
		if err := checkTag(tag, mod+"/v2"); err == nil {
			t.Errorf("checkTag(%q, /v2) = nil, want error", tag)
		}
	}
}

const changelog = `# Changelog

## [Unreleased]

- Pending.

## [1.2.0-rc.2] - 2026-10-01

- Second candidate.

## [1.2.0] - 2026-10-10

- Final.

## [1.1.0] - 2026-09-01

## [1.0.0] - 2026-08-01

- First.

[Unreleased]: https://example.com/compare/v1.2.0...HEAD
[1.2.0]: https://example.com/v1.2.0
`

func TestReleaseNotes(t *testing.T) {
	tests := []struct {
		tag, want string
	}{
		{"v1.2.0", "- Final."},
		{"v1.2.0-rc.2", "- Second candidate."},
		{"v1.2.0-rc.1", "- Final."}, // falls back to the base version
		{"v1.0.0", "- First."},      // last section stops at the links
	}
	for _, tt := range tests {
		got, err := releaseNotes(changelog, tt.tag)
		if err != nil {
			t.Errorf("releaseNotes(%q): %v", tt.tag, err)
			continue
		}
		if strings.TrimSpace(got) != tt.want {
			t.Errorf("releaseNotes(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
	for _, tag := range []string{"v1.1.0", "v1.3.0", "v1.3.0-rc.1", "bad"} {
		if got, err := releaseNotes(changelog, tag); err == nil {
			t.Errorf("releaseNotes(%q) = %q, want error", tag, got)
		}
	}
}

func TestCheckZip(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/m\n\ngo 1.26.0\n")
	write("m.go", "package m\n")
	write("sub/go.mod", "module example.com/m/sub\n") // nested module: omitted
	var out strings.Builder
	if err := checkZip(&out, dir); err != nil {
		t.Fatalf("checkZip: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "omitted: sub") || !strings.Contains(out.String(), "zip size") {
		t.Errorf("unexpected output:\n%s", out.String())
	}

	write("testdata/nul.txt", "x") // reserved on Windows
	out.Reset()
	if err := checkZip(&out, dir); err == nil {
		t.Errorf("checkZip accepted a Windows-reserved name:\n%s", out.String())
	}
}
