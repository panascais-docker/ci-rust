package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

func mergeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "merge",
		Short: "Tag the pushed amd64 and arm64 digests of every built line as multi-platform images",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return merge() },
	}
}

func merge() error {
	tags, err := readConfiguration(tagsFile)
	if err != nil {
		return err
	}

	digests, err := readConfiguration(digestsFile)
	if err != nil {
		return err
	}

	pushed, err := readPushed()
	if err != nil {
		return err
	}

	creations, err := planMerges(pushed, tags, digests)
	if err != nil {
		return err
	}

	for _, registry := range registries {
		if err := registry.login(); err != nil {
			return err
		}
	}

	errs := make([]error, len(creations))

	var group sync.WaitGroup
	for index, creation := range creations {
		group.Go(func() { errs[index] = command("docker", creation...).Run() })
	}
	group.Wait()

	return errors.Join(errs...)
}

func readPushed() (map[string]configuration, error) {
	files, err := filepath.Glob(filepath.Join(digestsDirectory, "*.json"))
	if err != nil {
		return nil, err
	}

	pushed := map[string]configuration{}
	for _, file := range files {
		line, architecture, _ := strings.Cut(strings.TrimSuffix(filepath.Base(file), ".json"), "-")

		value, err := readConfiguration(file)
		if err != nil {
			return nil, err
		}

		if pushed[architecture] == nil {
			pushed[architecture] = configuration{}
		}

		for variant, digest := range value[line] {
			pushed[architecture].set(line, variant, digest)
		}
	}

	return pushed, nil
}

func planMerges(pushed map[string]configuration, tags, digests configuration) ([][]string, error) {
	lines := map[string]bool{}
	for _, architecture := range architectures {
		for line := range pushed[architecture] {
			lines[line] = true
		}
	}

	if len(lines) == 0 {
		return nil, fmt.Errorf("no pushed digests found in %s", digestsDirectory)
	}

	var creations [][]string
	for _, line := range sortedKeys(lines) {
		builds, err := planBuilds(line, tags, digests)
		if err != nil {
			return nil, err
		}

		for _, build := range builds {
			var sources []string
			for _, architecture := range architectures {
				digest := pushed[architecture][line][build.variant]
				if digest == "" {
					return nil, fmt.Errorf("no pushed %s digest for %s", architecture, build.target())
				}

				sources = append(sources, digest)
			}

			for _, registry := range registries {
				creation := []string{"buildx", "imagetools", "create"}
				for _, tag := range build.tags(registry) {
					creation = append(creation, "--tag", tag)
				}

				for _, source := range sources {
					creation = append(creation, registry.image+"@"+source)
				}

				creations = append(creations, creation)
			}
		}
	}

	return creations, nil
}
