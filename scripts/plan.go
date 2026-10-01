package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/spf13/cobra"
)

type staleBase struct {
	Variant      string `json:"variant"`
	Architecture string `json:"architecture"`
}

func planCommand() *cobra.Command {
	var force bool

	command := &cobra.Command{
		Use:   "plan",
		Short: "Print the stale bases and lines as JSON arrays in GitHub Actions output format",
		Args:  cobra.NoArgs,
		RunE:  func(command *cobra.Command, _ []string) error { return plan(command.Context(), force) },
	}
	command.Flags().BoolVar(&force, "force", false, "treat every base and line as stale without reading the registry")

	return command
}

func plan(ctx context.Context, force bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	sources, err := readSources()
	if err != nil {
		return err
	}

	expectedBases := configuration{}
	for _, variant := range sortedKeys(sources.basePins) {
		for _, architecture := range architectures {
			expectedBases.set(variant, architecture, sources.baseFingerprint(variant, architecture))
		}
	}

	baseDigests, publishedBases, publishedLines := configuration{}, configuration{}, configuration{}
	if !force {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			if err := registries[0].login(); err != nil {
				return err
			}
		}

		for _, variant := range sortedKeys(sources.basePins) {
			for _, architecture := range architectures {
				digest, label, err := fetchBase(ctx, variant, architecture)
				if missing(err) {
					_, _ = fmt.Fprintf(os.Stderr, "%s:%s-%s is missing\n", baseImage, variant, architecture)

					continue
				}

				if err != nil {
					return err
				}

				baseDigests.set(variant, architecture, digest)
				publishedBases.set(variant, architecture, label)
			}
		}

		for _, line := range sortedKeys(sources.lines) {
			for _, variant := range sortedKeys(sources.basePins) {
				annotation, err := fetchLineAnnotation(ctx, sources.lines[line]["version"]+"-"+variant)
				if missing(err) {
					_, _ = fmt.Fprintf(os.Stderr, "%s:%s-%s is missing\n", registries[0].image, sources.lines[line]["version"], variant)

					continue
				}

				if err != nil {
					return err
				}

				publishedLines.set(line, variant, annotation)
			}
		}
	}

	bases, lines := planStale(expectedBases, publishedBases, lineFingerprints(sources, baseDigests), publishedLines, force)

	encodedBases, err := json.Marshal(bases)
	if err != nil {
		return err
	}

	encodedLines, err := json.Marshal(lines)
	if err != nil {
		return err
	}

	fmt.Println("bases=" + string(encodedBases))
	fmt.Println("lines=" + string(encodedLines))

	return nil
}

func fetchLineAnnotation(ctx context.Context, tag string) (string, error) {
	manifest, err := crane.Manifest(registries[0].image+":"+tag, crane.WithContext(ctx))
	if err != nil {
		return "", err
	}

	index, err := v1.ParseIndexManifest(bytes.NewReader(manifest))
	if err != nil {
		return "", err
	}

	return index.Annotations[lineAnnotation], nil
}

func planStale(expectedBases, publishedBases, expectedLines, publishedLines configuration, force bool) ([]staleBase, []string) {
	bases := []staleBase{}
	staleVariants := map[string]bool{}
	for _, variant := range sortedKeys(expectedBases) {
		for _, architecture := range architectures {
			if force || publishedBases[variant][architecture] != expectedBases[variant][architecture] {
				bases = append(bases, staleBase{Variant: variant, Architecture: architecture})
				staleVariants[variant] = true
			}
		}
	}

	lines := []string{}
	for _, line := range slices.Backward(sortedKeys(expectedLines)) {
		for _, variant := range sortedKeys(expectedBases) {
			if force || staleVariants[variant] || publishedLines[line][variant] != expectedLines[line][variant] {
				lines = append(lines, line)

				break
			}
		}
	}

	return bases, lines
}
