package main

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestChangedLines(t *testing.T) {
	digestsBefore := configuration{"1.97": {"alpine": "sha256:a"}, "1.98": {"alpine": "sha256:b"}, "1.85": {"bookworm": "sha256:c"}}
	tagsBefore := configuration{"1.97": {"alpine": "1.97.1-alpine3.24"}, "1.98": {"alpine": "1.98.0-alpine3.24"}, "1.85": {"bookworm": "1.85.1-slim-bookworm"}}

	digests := configuration{"1.97": {"alpine": "sha256:a"}, "1.98": {"alpine": "sha256:d"}, "1.99": {"alpine": "sha256:e"}}
	tags := configuration{"1.97": {"alpine": "1.97.1-alpine3.24"}, "1.98": {"alpine": "1.98.0-alpine3.24"}, "1.99": {"alpine": "1.99.0-alpine3.24"}}

	if lines := changedLines(digests, tags, digestsBefore, tagsBefore); !slices.Equal(lines, []string{"1.98", "1.99"}) {
		t.Errorf("changedLines() = %v, expected [1.98 1.99]", lines)
	}

	if lines := changedLines(digests, tags, digests, tags); lines == nil || len(lines) != 0 {
		t.Errorf("changedLines() = %#v, expected an empty non-nil slice", lines)
	}
}

func TestResolveReleases(t *testing.T) {
	releases := resolveReleases([]string{
		"1.84.1-alpine3.21",
		"1.85.0-alpine3.21",
		"1.85.1-alpine3.20",
		"1.85.1-alpine3.21",
		"1.85.1-slim-bookworm",
		"1.85.1-bookworm",
		"1.85.1-slim-bullseye",
		"1.88.0-slim-trixie",
		"1.88.0-alpine3.22",
		"1.88-alpine3.22",
		"1.88.0-alpine",
		"1.88.0-slim",
		"1-alpine3.22",
		"alpine3.24",
		"latest",
	})

	expected := map[string]map[string]release{
		"1.85": {"alpine": {patch: 1, alpine: 21, tag: "1.85.1-alpine3.21"}, "bookworm": {patch: 1, tag: "1.85.1-slim-bookworm"}},
		"1.88": {"alpine": {patch: 0, alpine: 22, tag: "1.88.0-alpine3.22"}, "trixie": {patch: 0, tag: "1.88.0-slim-trixie"}},
	}

	if !maps.EqualFunc(releases, expected, maps.Equal) {
		t.Errorf("resolveReleases() = %v, expected %v", releases, expected)
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
