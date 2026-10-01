package main

import (
	"slices"
	"testing"
)

func TestPlanStale(t *testing.T) {
	expectedBases := configuration{
		"alpine": {"amd64": "sha256:a", "arm64": "sha256:b"},
		"trixie": {"amd64": "sha256:c", "arm64": "sha256:d"},
	}
	expectedLines := configuration{
		"1.85": {"alpine": "sha256:e", "trixie": "sha256:f"},
		"1.98": {"alpine": "sha256:g", "trixie": "sha256:h"},
	}

	for _, testCase := range []struct {
		name                           string
		publishedBases, publishedLines configuration
		force                          bool
		bases                          []staleBase
		lines                          []string
	}{
		{
			name:           "current",
			publishedBases: expectedBases,
			publishedLines: expectedLines,
			bases:          []staleBase{},
			lines:          []string{},
		},
		{
			name:           "force",
			publishedBases: expectedBases,
			publishedLines: expectedLines,
			force:          true,
			bases:          []staleBase{{"alpine", "amd64"}, {"alpine", "arm64"}, {"trixie", "amd64"}, {"trixie", "arm64"}},
			lines:          []string{"1.98", "1.85"},
		},
		{
			name:           "missing images",
			publishedBases: configuration{},
			publishedLines: configuration{},
			bases:          []staleBase{{"alpine", "amd64"}, {"alpine", "arm64"}, {"trixie", "amd64"}, {"trixie", "arm64"}},
			lines:          []string{"1.98", "1.85"},
		},
		{
			name:           "stale base",
			publishedBases: configuration{"alpine": {"amd64": "sha256:a", "arm64": "sha256:b"}, "trixie": {"amd64": "sha256:c", "arm64": "sha256:x"}},
			publishedLines: expectedLines,
			bases:          []staleBase{{"trixie", "arm64"}},
			lines:          []string{"1.98", "1.85"},
		},
		{
			name:           "missing line",
			publishedBases: expectedBases,
			publishedLines: configuration{"1.85": {"alpine": "sha256:e", "trixie": "sha256:f"}, "1.98": {"alpine": "sha256:g"}},
			bases:          []staleBase{},
			lines:          []string{"1.98"},
		},
		{
			name:           "stale line",
			publishedBases: expectedBases,
			publishedLines: configuration{"1.85": {"alpine": "sha256:x", "trixie": "sha256:f"}, "1.98": {"alpine": "sha256:g", "trixie": "sha256:h"}},
			bases:          []staleBase{},
			lines:          []string{"1.85"},
		},
	} {
		bases, lines := planStale(expectedBases, testCase.publishedBases, expectedLines, testCase.publishedLines, testCase.force)
		if bases == nil || lines == nil || !slices.Equal(bases, testCase.bases) || !slices.Equal(lines, testCase.lines) {
			t.Errorf("planStale(%s) = %v, %v, expected %v, %v", testCase.name, bases, lines, testCase.bases, testCase.lines)
		}
	}
}
