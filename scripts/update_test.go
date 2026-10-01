package main

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestChangedLines(t *testing.T) {
	linesBefore := configuration{"1.97": {"version": "1.97.1"}, "1.98": {"version": "1.98.0"}, "1.85": {"version": "1.85.1"}}
	lines := configuration{"1.97": {"version": "1.97.1"}, "1.98": {"version": "1.98.1"}, "1.99": {"version": "1.99.0"}}

	if changed := changedLines(lines, linesBefore); !slices.Equal(changed, []string{"1.98", "1.99"}) {
		t.Errorf("changedLines() = %v, expected [1.98 1.99]", changed)
	}

	if changed := changedLines(lines, lines); changed == nil || len(changed) != 0 {
		t.Errorf("changedLines() = %#v, expected an empty non-nil slice", changed)
	}
}

func TestResolveLines(t *testing.T) {
	lines := resolveLines(strings.Join([]string{
		"static.rust-lang.org/dist/2025-01-09/channel-rust-1.84.0.toml",
		"static.rust-lang.org/dist/2025-02-20/channel-rust-1.85.0.toml",
		"static.rust-lang.org/dist/2025-03-18/channel-rust-1.85.1.toml",
		"static.rust-lang.org/dist/2025-03-18/channel-rust-1.85.toml",
		"static.rust-lang.org/dist/2026-09-03/channel-rust-1.98.1.toml",
		"static.rust-lang.org/dist/2026-09-03/channel-rust-1.98.0.toml",
		"static.rust-lang.org/dist/2026-09-20/channel-rust-1.99.0-beta.6.toml",
		"static.rust-lang.org/dist/2026-09-20/channel-rust-1.99.0-beta.toml",
		"static.rust-lang.org/dist/2026-10-01/channel-rust-stable.toml",
		"static.rust-lang.org/dist/2026-10-01/channel-rust-nightly.toml",
	}, "\n"))

	if expected := (configuration{"1.85": {"version": "1.85.1"}, "1.98": {"version": "1.98.1"}}); !lines.equal(expected) {
		t.Errorf("resolveLines() = %v, expected %v", lines, expected)
	}
}

func TestPin(t *testing.T) {
	digest := func(character string) string { return "sha256:" + strings.Repeat(character, 64) }
	just := tool{name: "just", repository: "casey/just", release: regexp.MustCompile(`^(\d+\.\d+\.\d+)$`), asset: "just-%s-%s-unknown-linux-musl.tar.gz"}

	pin, err := just.pin([]githubRelease{
		{TagName: "1.59.0", Prerelease: true},
		{TagName: "1.58.0", Assets: []githubAsset{
			{Name: "just-1.58.0-aarch64-unknown-linux-gnu.tar.gz", Digest: digest("c")},
			{Name: "just-1.58.0-aarch64-unknown-linux-musl.tar.gz", Digest: digest("b")},
			{Name: "just-1.58.0-x86_64-unknown-linux-musl.tar.gz", Digest: digest("a")},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if expected := map[string]string{"version": "1.58.0", "amd64": digest("a"), "arm64": digest("b")}; !maps.Equal(pin, expected) {
		t.Errorf("pin() = %v, expected %v", pin, expected)
	}

	if _, err := just.pin([]githubRelease{{TagName: "1.58.0", Assets: []githubAsset{{Name: "just-1.58.0-x86_64-unknown-linux-musl.tar.gz", Digest: digest("a")}}}}); err == nil {
		t.Error("pin() without an arm64 asset succeeded, expected an error")
	}
}

func TestSortedKeys(t *testing.T) {
	if keys := sortedKeys(map[string]int{"1.9": 0, "1.85": 0, "1.10": 0, "sccache": 0, "just": 0}); !slices.Equal(keys, []string{"just", "sccache", "1.9", "1.10", "1.85"}) {
		t.Errorf("sortedKeys() = %v", keys)
	}
}
