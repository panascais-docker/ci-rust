package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/spf13/cobra"
)

const baseImage = "ghcr.io/panascais-docker/ci-rust/base"

func baseCommand() *cobra.Command {
	var platform string

	command := &cobra.Command{
		Use:   "base <variant>",
		Short: "Build the base image of a variant, pushing it to GitHub Container Registry on GitHub Actions and loading locally",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, arguments []string) error { return buildBase(arguments[0], platform) },
	}
	command.Flags().StringVar(&platform, "platform", "linux/"+runtime.GOARCH, "platform to build")

	return command
}

func buildBase(variant, platform string) error {
	sources, err := readSources()
	if err != nil {
		return err
	}

	image, digest := sources.basePins[variant]["image"], sources.basePins[variant]["digest"]
	if image == "" || !digestPattern.MatchString(digest) {
		return fmt.Errorf("invalid variant %q, expected one of %s", variant, strings.Join(sortedKeys(sources.basePins), ", "))
	}

	_, architecture, _ := strings.Cut(platform, "/")
	if !slices.Contains(architectures, architecture) {
		return fmt.Errorf("invalid platform %q, expected linux/%s", platform, strings.Join(architectures, " or linux/"))
	}

	arguments := buildArguments(sources.toolPins, "local")
	arguments["BASE_FINGERPRINT"] = sources.baseFingerprint(variant, architecture)

	builds := []build{{line: "base", variant: variant, base: image + "@" + digest}}
	tag := baseImage + ":" + variant + "-" + architecture

	if os.Getenv("GITHUB_ACTIONS") != "true" {
		load := func(build) ([]string, []string) { return []string{tag}, nil }

		return bake(builds, arguments, platform, "base", load, "--load")
	}

	for _, registry := range registries {
		if err := registry.login(); err != nil {
			return err
		}
	}

	push := func(build) ([]string, []string) { return nil, []string{"type=image,name=" + tag + ",push=true"} }

	return bake(builds, arguments, platform, "base", push)
}

func fetchBase(ctx context.Context, variant, architecture string) (digest, label string, err error) {
	image, err := crane.Pull(baseImage+":"+variant+"-"+architecture, crane.WithContext(ctx), crane.WithPlatform(&v1.Platform{OS: "linux", Architecture: architecture}))
	if err != nil {
		return "", "", err
	}

	imageDigest, err := image.Digest()
	if err != nil {
		return "", "", err
	}

	config, err := image.ConfigFile()
	if err != nil {
		return "", "", err
	}

	return imageDigest.String(), config.Config.Labels[baseLabel], nil
}

func fetchBaseDigests(ctx context.Context, variants []string) (configuration, error) {
	digests := configuration{}
	for _, variant := range variants {
		for _, architecture := range architectures {
			digest, _, err := fetchBase(ctx, variant, architecture)
			if err != nil {
				return nil, err
			}

			digests.set(variant, architecture, digest)
		}
	}

	return digests, nil
}

func missing(err error) bool {
	transportError, found := errors.AsType[*transport.Error](err)
	if !found {
		return false
	}

	return transportError.StatusCode == http.StatusNotFound || slices.ContainsFunc(transportError.Errors, func(diagnostic transport.Diagnostic) bool {
		return diagnostic.Code == transport.ManifestUnknownErrorCode
	})
}

func lineFingerprints(sources sources, baseDigests configuration) configuration {
	fingerprints := configuration{}
	for line := range sources.lines {
		for variant := range sources.basePins {
			fingerprints.set(line, variant, sources.lineFingerprint(line, baseDigests[variant]))
		}
	}

	return fingerprints
}
