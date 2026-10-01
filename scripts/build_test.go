package main

import (
	"slices"
	"strings"
	"testing"
)

func TestResolveNames(t *testing.T) {
	tags := configuration{
		"1.85": {"alpine": "1.85.1-alpine3.21", "bookworm": "1.85.1-slim-bookworm"},
		"1.88": {"alpine": "1.88.0-alpine3.22", "bookworm": "1.88.0-slim-bookworm", "trixie": "1.88.0-slim-trixie"},
		"1.98": {"alpine": "1.98.1-alpine3.24", "bookworm": "1.98.1-slim-bookworm", "trixie": "1.98.1-slim-trixie"},
	}

	for _, testCase := range []struct {
		line, variant, version string
		expected               []string
	}{
		{"1.85", "alpine", "1.85.1", []string{"1.85-alpine", "1.85.1-alpine", "1.85", "1.85.1"}},
		{"1.85", "bookworm", "1.85.1", []string{"1.85-bookworm", "1.85.1-bookworm"}},
		{"1.88", "trixie", "1.88.0", []string{"1.88-trixie", "1.88.0-trixie"}},
		{"1.98", "alpine", "1.98.1", []string{"1.98-alpine", "1.98.1-alpine", "1-alpine", "latest-alpine", "1.98", "1.98.1", "1", "latest"}},
		{"1.98", "trixie", "1.98.1", []string{"1.98-trixie", "1.98.1-trixie", "1-trixie", "latest-trixie"}},
	} {
		if actual := resolveNames(tags, testCase.line, testCase.variant, testCase.version); !slices.Equal(actual, testCase.expected) {
			t.Errorf("resolveNames(%s, %s) = %q, expected %q", testCase.line, testCase.variant, actual, testCase.expected)
		}
	}
}

func TestValidatePin(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)

	for _, testCase := range []struct {
		variant, tag string
		valid        bool
	}{
		{"alpine", "1.98.1-alpine3.24", true},
		{"alpine", "1.98.1-alpine", false},
		{"trixie", "1.98.1-slim-trixie", true},
		{"trixie", "1.98.1-trixie", false},
		{"bookworm", "1.97.1-slim-bookworm", false},
	} {
		if err := validatePin("1.98", testCase.variant, testCase.tag, digest); (err == nil) != testCase.valid {
			t.Errorf("validatePin(1.98, %s, %s) = %v, expected valid %t", testCase.variant, testCase.tag, err, testCase.valid)
		}
	}
}

func TestPlanManifests(t *testing.T) {
	digest := func(character string) string { return "sha256:" + strings.Repeat(character, 64) }
	tags := configuration{"1.98": {"alpine": "1.98.1-alpine3.24"}}
	digests := configuration{"1.98": {"alpine": digest("a")}}

	creations, err := planManifests(map[string]configuration{
		"amd64": {"1.98": {"alpine": digest("b")}},
		"arm64": {"1.98": {"alpine": digest("c")}},
	}, tags, digests)
	if err != nil {
		t.Fatal(err)
	}

	if len(creations) != len(registries) {
		t.Fatalf("planManifests() = %d creations, expected %d", len(creations), len(registries))
	}

	expected := []string{
		"buildx", "imagetools", "create",
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

	if _, err := planManifests(map[string]configuration{"amd64": {"1.98": {"alpine": digest("b")}}}, tags, digests); err == nil {
		t.Error("planManifests() without an arm64 digest succeeded, expected an error")
	}
}
