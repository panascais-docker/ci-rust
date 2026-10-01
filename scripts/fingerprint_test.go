package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprint(t *testing.T) {
	expected := "sha256:4a73850fde34aad40ff8649b93a66523a5fe744357a3931caea0f10609d0d930"
	if actual := fingerprint(map[string]string{"b": "2", "a": "1"}); actual != expected {
		t.Errorf("fingerprint() = %s, expected %s", actual, expected)
	}
}

func TestBaseFingerprint(t *testing.T) {
	sources := sources{
		basePins:   configuration{"alpine": {"image": "library/alpine:3.24", "digest": "sha256:a"}},
		toolPins:   configuration{"just": {"version": "1.58.0", "amd64": "sha256:b", "arm64": "sha256:c"}},
		dockerfile: "sha256:d",
	}

	expected := fingerprint(map[string]string{
		"architecture": "arm64",
		"base":         "library/alpine:3.24@sha256:a",
		"dockerfile":   "sha256:d",
		"just.version": "1.58.0",
		"just.amd64":   "sha256:b",
		"just.arm64":   "sha256:c",
	})
	if actual := sources.baseFingerprint("alpine", "arm64"); actual != expected {
		t.Errorf("baseFingerprint(alpine, arm64) = %s, expected %s", actual, expected)
	}

	if sources.baseFingerprint("alpine", "amd64") == expected {
		t.Error("baseFingerprint(alpine, amd64) equals the arm64 fingerprint")
	}
}

func TestLineFingerprint(t *testing.T) {
	sources := sources{lines: configuration{"1.98": {"version": "1.98.1"}}, dockerfile: "sha256:d", smoke: "sha256:e"}

	expected := fingerprint(map[string]string{
		"version":    "1.98.1",
		"base.amd64": "sha256:a",
		"base.arm64": "sha256:b",
		"dockerfile": "sha256:d",
		"smoke":      "sha256:e",
	})
	if actual := sources.lineFingerprint("1.98", map[string]string{"amd64": "sha256:a", "arm64": "sha256:b"}); actual != expected {
		t.Errorf("lineFingerprint(1.98) = %s, expected %s", actual, expected)
	}
}

func TestSmokeFingerprint(t *testing.T) {
	t.Chdir(t.TempDir())

	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("smoke/Cargo.toml", "[package]")
	write("smoke/src/main.rs", "fn main() {}")

	before, err := smokeFingerprint()
	if err != nil {
		t.Fatal(err)
	}

	expected := fingerprint(map[string]string{
		"Cargo.toml":  "sha256:70acf00586aa7b90c3866278505be7b81fb7ee1f7e21c17c1a22ac239be3c72a",
		"src/main.rs": "sha256:ef32637cb9c3ec2e3968c9cbdf26a5e9c172be94f88af533e14bd43f892d5297",
	})
	if before != expected {
		t.Errorf("smokeFingerprint() = %s, expected %s", before, expected)
	}

	write("smoke/target/release/smoke", "binary")
	if after, err := smokeFingerprint(); err != nil || after != before {
		t.Errorf("smokeFingerprint() with a target directory = %s, %v, expected %s", after, err, before)
	}

	write("smoke/src/main.rs", "fn main() { println!() }")
	if after, err := smokeFingerprint(); err != nil || after == before {
		t.Errorf("smokeFingerprint() after editing a source = %s, %v, expected a change", after, err)
	}
}
