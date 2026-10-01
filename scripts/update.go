package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/spf13/cobra"
)

const minimumLine = "1.85"

var (
	architectures  = []string{"amd64", "arm64"}
	triples        = map[string]string{"amd64": "x86_64", "arm64": "aarch64"}
	digestPattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	releasePattern = regexp.MustCompile(`^(\d+\.\d+)\.(\d+)-(?:alpine3\.(\d+)|slim-(trixie|bookworm))$`)
	tools          = []tool{
		{name: "cargo-deny", repository: "EmbarkStudios/cargo-deny", release: regexp.MustCompile(`^(\d+\.\d+\.\d+)$`), asset: "cargo-deny-%s-%s-unknown-linux-musl.tar.gz"},
		{name: "cargo-nextest", repository: "nextest-rs/nextest", release: regexp.MustCompile(`^cargo-nextest-(\d+\.\d+\.\d+)$`), asset: "cargo-nextest-%s-%s-unknown-linux-musl.tar.gz"},
		{name: "just", repository: "casey/just", release: regexp.MustCompile(`^(\d+\.\d+\.\d+)$`), asset: "just-%s-%s-unknown-linux-musl.tar.gz"},
		{name: "sccache", repository: "mozilla/sccache", release: regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`), asset: "sccache-v%s-%s-unknown-linux-musl.tar.gz"},
	}
)

type release struct {
	patch  int
	alpine int
	tag    string
}

type tool struct {
	name       string
	repository string
	release    *regexp.Regexp
	asset      string
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Draft      bool          `json:"draft"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

func updateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: `Pin the newest upstream image per line and variant and the newest tools, printing the changed lines as a JSON array`,
		Args:  cobra.NoArgs,
		RunE:  func(command *cobra.Command, _ []string) error { return update(command.Context()) },
	}
}

func update(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	digestsBefore, err := readConfiguration(digestsFile)
	if err != nil {
		return err
	}

	tagsBefore, err := readConfiguration(tagsFile)
	if err != nil {
		return err
	}

	toolPinsBefore, err := readConfiguration(toolsFile)
	if err != nil {
		return err
	}

	names, err := crane.ListTags(repository, dockerHub(ctx)...)
	if err != nil {
		return err
	}

	digests, tags, err := resolvePins(ctx, resolveReleases(names), digestsBefore, tagsBefore)
	if err != nil {
		return err
	}

	toolPins, err := resolveTools(ctx)
	if err != nil {
		return err
	}

	if digests.equal(digestsBefore) && tags.equal(tagsBefore) && toolPins.equal(toolPinsBefore) {
		fmt.Println("[]")

		return nil
	}

	if err := writeConfiguration(digestsFile, digests); err != nil {
		return err
	}

	if err := writeConfiguration(tagsFile, tags); err != nil {
		return err
	}

	if err := writeConfiguration(toolsFile, toolPins); err != nil {
		return err
	}

	changed := changedLines(digests, tags, digestsBefore, tagsBefore)
	if !toolPins.equal(toolPinsBefore) {
		changed = sortedKeys(tags)
	}

	lines, err := json.Marshal(changed)
	if err != nil {
		return err
	}

	fmt.Println(string(lines))

	return nil
}

func changedLines(digests, tags, digestsBefore, tagsBefore configuration) []string {
	lines := []string{}
	for _, line := range sortedKeys(tags) {
		if digests.changed(digestsBefore, line) || tags.changed(tagsBefore, line) {
			lines = append(lines, line)
		}
	}

	return lines
}

func resolvePins(ctx context.Context, releases map[string]map[string]release, digestsBefore, tagsBefore configuration) (configuration, configuration, error) {
	digests, tags := configuration{}, configuration{}
	for line, variants := range releases {
		for variant, release := range variants {
			digest, err := fetchDigest(ctx, release.tag, digestsBefore[line][variant])
			if err != nil {
				return nil, nil, err
			}

			tag := release.tag
			if digest == "" {
				digest, tag = digestsBefore[line][variant], tagsBefore[line][variant]
			}

			if digest == "" || tag == "" {
				continue
			}

			digests.set(line, variant, digest)
			tags.set(line, variant, tag)
		}
	}

	return digests, tags, nil
}

func resolveReleases(names []string) map[string]map[string]release {
	releases := map[string]map[string]release{}
	for _, name := range names {
		match := releasePattern.FindStringSubmatch(name)
		if match == nil || compareKeys(match[1], minimumLine) < 0 {
			continue
		}

		line, variant := match[1], cmp.Or(match[4], "alpine")
		patch, _ := strconv.Atoi(match[2])
		alpine, _ := strconv.Atoi(match[3])

		if releases[line] == nil {
			releases[line] = map[string]release{}
		}

		current, found := releases[line][variant]
		if !found || cmp.Or(cmp.Compare(current.patch, patch), cmp.Compare(current.alpine, alpine)) < 0 {
			releases[line][variant] = release{patch: patch, alpine: alpine, tag: name}
		}
	}

	return releases
}

func fetchDigest(ctx context.Context, tag, pinned string) (string, error) {
	head, err := crane.Head(repository+":"+tag, dockerHub(ctx)...)
	if err != nil || head.Digest.String() == pinned {
		return pinned, err
	}

	manifest, err := crane.Manifest(repository+"@"+head.Digest.String(), dockerHub(ctx)...)
	if err != nil {
		return "", err
	}

	index, err := v1.ParseIndexManifest(bytes.NewReader(manifest))
	if err != nil {
		return "", err
	}

	for _, architecture := range architectures {
		if !slices.ContainsFunc(index.Manifests, func(entry v1.Descriptor) bool {
			return entry.Platform != nil && entry.Platform.Architecture == architecture
		}) {
			return "", nil
		}
	}

	return head.Digest.String(), nil
}

func dockerHub(ctx context.Context) []crane.Option {
	return []crane.Option{crane.WithContext(ctx), func(options *crane.Options) {
		options.Name = append(options.Name, name.WithDefaultRegistry("registry-1.docker.io"))
		options.Remote = append(options.Remote, remote.WithPageSize(1_000_000))
	}}
}

func resolveTools(ctx context.Context) (configuration, error) {
	pins := configuration{}
	for _, tool := range tools {
		releases, err := tool.fetchReleases(ctx)
		if err != nil {
			return nil, err
		}

		pin, err := tool.pin(releases)
		if err != nil {
			return nil, err
		}

		pins[tool.name] = pin
	}

	return pins, nil
}

func (tool tool) pin(releases []githubRelease) (map[string]string, error) {
	for _, release := range releases {
		match := tool.release.FindStringSubmatch(release.TagName)
		if match == nil || release.Draft || release.Prerelease {
			continue
		}

		pin := map[string]string{"version": match[1]}
		for _, architecture := range architectures {
			asset := fmt.Sprintf(tool.asset, match[1], triples[architecture])
			index := slices.IndexFunc(release.Assets, func(candidate githubAsset) bool { return candidate.Name == asset })
			if index < 0 || !digestPattern.MatchString(release.Assets[index].Digest) {
				return nil, fmt.Errorf("%s %s has no %s with a sha256 digest", tool.name, match[1], asset)
			}

			pin[architecture] = release.Assets[index].Digest
		}

		return pin, nil
	}

	return nil, fmt.Errorf("no %s release found in %s", tool.name, tool.repository)
}

func (tool tool) fetchReleases(ctx context.Context) ([]githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+tool.repository+"/releases", nil)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s releases: %s", tool.repository, response.Status)
	}

	var releases []githubRelease

	return releases, json.NewDecoder(response.Body).Decode(&releases)
}
