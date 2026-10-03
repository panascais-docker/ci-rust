package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

const (
	dockerfile       = "Dockerfile"
	smokeDirectory   = "smoke"
	baseLabel        = "net.panascais.docker.ci-rust.base-fingerprint"
	lineAnnotation   = "net.panascais.docker.ci-rust.fingerprint"
	targetsDirectory = "target"
)

type sources struct {
	basePins   configuration
	lines      configuration
	toolPins   configuration
	dockerfile string
	smoke      string
}

func readSources() (sources, error) {
	basePins, err := readConfiguration(basesFile)
	if err != nil {
		return sources{}, err
	}

	lines, err := readConfiguration(linesFile)
	if err != nil {
		return sources{}, err
	}

	toolPins, err := readConfiguration(toolsFile)
	if err != nil {
		return sources{}, err
	}

	dockerfileDigest, err := fileDigest(dockerfile)
	if err != nil {
		return sources{}, err
	}

	smoke, err := smokeFingerprint()
	if err != nil {
		return sources{}, err
	}

	return sources{basePins: basePins, lines: lines, toolPins: toolPins, dockerfile: dockerfileDigest, smoke: smoke}, nil
}

func (sources sources) baseFingerprint(variant, architecture string) string {
	values := map[string]string{
		"architecture": architecture,
		"base":         sources.basePins[variant]["image"] + "@" + sources.basePins[variant]["digest"],
		"dockerfile":   sources.dockerfile,
	}

	for tool, pin := range sources.toolPins {
		for field, value := range pin {
			values[tool+"."+field] = value
		}
	}

	return fingerprint(values)
}

func (sources sources) lineFingerprint(line string, baseDigests map[string]string) string {
	values := map[string]string{
		"version":    sources.lines[line]["version"],
		"dockerfile": sources.dockerfile,
		"smoke":      sources.smoke,
	}

	for architecture, digest := range baseDigests {
		values["base."+architecture] = digest
	}

	return fingerprint(values)
}

func smokeFingerprint() (string, error) {
	values := map[string]string{}
	err := filepath.WalkDir(smokeDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(smokeDirectory, path)
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if relative == targetsDirectory {
				return filepath.SkipDir
			}

			return nil
		}

		digest, err := fileDigest(path)
		values[filepath.ToSlash(relative)] = digest

		return err
	})

	return fingerprint(values), err
}

func fileDigest(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(content)

	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func fingerprint(values map[string]string) string {
	hash := sha256.New()
	for _, key := range slices.Sorted(maps.Keys(values)) {
		_, _ = fmt.Fprintf(hash, "%s=%s\n", key, values[key])
	}

	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
