package main

import (
	"slices"
	"strings"
	"testing"
)

func TestResolveNames(t *testing.T) {
	lines := []string{"1.85", "1.88", "1.98"}

	for _, testCase := range []struct {
		line, variant, version string
		expected               []string
	}{
		{"1.85", "alpine", "1.85.1", []string{"1.85-alpine", "1.85.1-alpine", "1.85", "1.85.1"}},
		{"1.85", "trixie", "1.85.1", []string{"1.85-trixie", "1.85.1-trixie"}},
		{"1.98", "alpine", "1.98.1", []string{"1.98-alpine", "1.98.1-alpine", "1-alpine", "latest-alpine", "1.98", "1.98.1", "1", "latest"}},
		{"1.98", "bookworm", "1.98.1", []string{"1.98-bookworm", "1.98.1-bookworm", "1-bookworm", "latest-bookworm"}},
	} {
		if actual := resolveNames(lines, testCase.line, testCase.variant, testCase.version); !slices.Equal(actual, testCase.expected) {
			t.Errorf("resolveNames(%s, %s) = %q, expected %q", testCase.line, testCase.variant, actual, testCase.expected)
		}
	}
}

func TestPlanBuilds(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	basePins := configuration{"alpine": {"image": "library/alpine:3.24", "digest": digest}, "trixie": {"image": "library/debian:trixie-slim", "digest": digest}}

	builds, err := planBuilds("1.98", configuration{"1.98": {"version": "1.98.1"}}, basePins)
	if err != nil {
		t.Fatal(err)
	}

	if len(builds) != 2 || builds[1].variant != "trixie" || builds[1].base != "library/debian:trixie-slim@"+digest || builds[1].version != "1.98.1" {
		t.Errorf("planBuilds() = %+v", builds)
	}

	for _, testCase := range []struct {
		lines, basePins configuration
	}{
		{configuration{"1.98": {"version": "1.97.1"}}, basePins},
		{configuration{"1.98": {"version": "1.98.1"}}, configuration{"alpine": {"image": "library/alpine:3.24", "digest": "latest"}}},
		{configuration{"1.97": {"version": "1.97.1"}}, basePins},
	} {
		if _, err := planBuilds("1.98", testCase.lines, testCase.basePins); err == nil {
			t.Errorf("planBuilds(1.98, %v, %v) succeeded, expected an error", testCase.lines, testCase.basePins)
		}
	}
}

func TestPlanManifests(t *testing.T) {
	digest := func(character string) string { return "sha256:" + strings.Repeat(character, 64) }
	lines := configuration{"1.98": {"version": "1.98.1"}}
	basePins := configuration{"alpine": {"image": "library/alpine:3.24", "digest": digest("a")}}
	fingerprints := configuration{"1.98": {"alpine": digest("d")}}

	creations, err := planManifests(map[string]configuration{
		"amd64": {"1.98": {"alpine": digest("b")}},
		"arm64": {"1.98": {"alpine": digest("c")}},
	}, lines, basePins, fingerprints)
	if err != nil {
		t.Fatal(err)
	}

	if len(creations) != len(registries) {
		t.Fatalf("planManifests() = %d creations, expected %d", len(creations), len(registries))
	}

	expected := []string{
		"buildx", "imagetools", "create",
		"--annotation", "index:org.panascais.ci-rust.fingerprint=" + digest("d"),
		"--tag", "quay.io/panascais/ci-rust:1.98-alpine",
		"--tag", "quay.io/panascais/ci-rust:1.98.1-alpine",
		"--tag", "quay.io/panascais/ci-rust:1-alpine",
		"--tag", "quay.io/panascais/ci-rust:latest-alpine",
		"--tag", "quay.io/panascais/ci-rust:1.98",
		"--tag", "quay.io/panascais/ci-rust:1.98.1",
		"--tag", "quay.io/panascais/ci-rust:1",
		"--tag", "quay.io/panascais/ci-rust:latest",
		"quay.io/panascais/ci-rust@" + digest("b"),
		"quay.io/panascais/ci-rust@" + digest("c"),
	}
	if !slices.Equal(creations[2], expected) {
		t.Errorf("planManifests() = %q, expected %q", creations[2], expected)
	}

	if _, err := planManifests(map[string]configuration{"amd64": {"1.98": {"alpine": digest("b")}}}, lines, basePins, fingerprints); err == nil {
		t.Error("planManifests() without an arm64 digest succeeded, expected an error")
	}

	if _, err := planManifests(map[string]configuration{
		"amd64": {"1.98": {"alpine": digest("b")}},
		"arm64": {"1.98": {"alpine": digest("c")}},
	}, lines, basePins, configuration{}); err == nil {
		t.Error("planManifests() without a fingerprint succeeded, expected an error")
	}
}
