package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/spf13/cobra"
)

const (
	minimumLine      = "1.85"
	manifestsURL     = "https://static.rust-lang.org/manifests.txt"
	rustupURL        = "https://static.rust-lang.org/rustup"
	rustupReleaseURL = rustupURL + "/release-stable.toml"
)

var (
	architectures   = []string{"amd64", "arm64"}
	triples         = map[string]string{"amd64": "x86_64", "arm64": "aarch64"}
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	manifestPattern = regexp.MustCompile(`/channel-rust-(\d+\.\d+)\.(\d+)\.toml$`)
	rustupPattern   = regexp.MustCompile(`(?m)^version = '(\d+\.\d+\.\d+)'$`)
	checksumPattern = regexp.MustCompile(`^([0-9a-f]{64})\s`)
	bases           = []base{
		{variant: "alpine", repository: "library/alpine", tag: regexp.MustCompile(`^3\.\d+$`)},
		{variant: "bookworm", repository: "library/debian", tag: regexp.MustCompile(`^bookworm-slim$`)},
		{variant: "trixie", repository: "library/debian", tag: regexp.MustCompile(`^trixie-slim$`)},
	}
	tools = []tool{
		{name: "cargo-deny", repository: "EmbarkStudios/cargo-deny", release: regexp.MustCompile(`^(\d+\.\d+\.\d+)$`), asset: "cargo-deny-%s-%s-unknown-linux-musl.tar.gz"},
		{name: "cargo-nextest", repository: "nextest-rs/nextest", release: regexp.MustCompile(`^cargo-nextest-(\d+\.\d+\.\d+)$`), asset: "cargo-nextest-%s-%s-unknown-linux-musl.tar.gz"},
		{name: "just", repository: "casey/just", release: regexp.MustCompile(`^(\d+\.\d+\.\d+)$`), asset: "just-%s-%s-unknown-linux-musl.tar.gz"},
		{name: "sccache", repository: "mozilla/sccache", release: regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`), asset: "sccache-v%s-%s-unknown-linux-musl.tar.gz"},
	}
)

type base struct {
	variant    string
	repository string
	tag        *regexp.Regexp
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
		Short: `Pin the newest base images, Rust patch per line and tools, printing the changed lines as a JSON array`,
		Args:  cobra.NoArgs,
		RunE:  func(command *cobra.Command, _ []string) error { return update(command.Context()) },
	}
}

func update(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	basesBefore, err := readConfiguration(basesFile)
	if err != nil {
		return err
	}

	linesBefore, err := readConfiguration(linesFile)
	if err != nil {
		return err
	}

	toolPinsBefore, err := readConfiguration(toolsFile)
	if err != nil {
		return err
	}

	manifests, err := fetch(ctx, manifestsURL, nil)
	if err != nil {
		return err
	}

	lines := resolveLines(string(manifests))

	basePins, err := resolveBases(ctx, basesBefore)
	if err != nil {
		return err
	}

	toolPins, err := resolveTools(ctx)
	if err != nil {
		return err
	}

	if lines.equal(linesBefore) && basePins.equal(basesBefore) && toolPins.equal(toolPinsBefore) {
		fmt.Println("[]")

		return nil
	}

	for path, value := range map[string]configuration{basesFile: basePins, linesFile: lines, toolsFile: toolPins} {
		if err := writeConfiguration(path, value); err != nil {
			return err
		}
	}

	changed := changedLines(lines, linesBefore)
	if !basePins.equal(basesBefore) || !toolPins.equal(toolPinsBefore) {
		changed = sortedKeys(lines)
	}

	encoded, err := json.Marshal(changed)
	if err != nil {
		return err
	}

	fmt.Println(string(encoded))

	return nil
}

func changedLines(lines, linesBefore configuration) []string {
	changed := []string{}
	for _, line := range sortedKeys(lines) {
		if lines.changed(linesBefore, line) {
			changed = append(changed, line)
		}
	}

	return changed
}

func resolveLines(manifests string) configuration {
	patches := map[string]int{}
	for manifest := range strings.FieldsSeq(manifests) {
		match := manifestPattern.FindStringSubmatch(manifest)
		if match == nil || compareKeys(match[1], minimumLine) < 0 {
			continue
		}

		patch, _ := strconv.Atoi(match[2])
		if current, found := patches[match[1]]; !found || current < patch {
			patches[match[1]] = patch
		}
	}

	lines := configuration{}
	for line, patch := range patches {
		lines.set(line, "version", line+"."+strconv.Itoa(patch))
	}

	return lines
}

func resolveBases(ctx context.Context, basesBefore configuration) (configuration, error) {
	pins := configuration{}
	for _, base := range bases {
		names, err := crane.ListTags(base.repository, dockerHub(ctx)...)
		if err != nil {
			return nil, err
		}

		matching := slices.DeleteFunc(names, func(tag string) bool { return !base.tag.MatchString(tag) })
		if len(matching) == 0 {
			return nil, fmt.Errorf("no %s tag in %s matches %s", base.variant, base.repository, base.tag)
		}

		image := base.repository + ":" + slices.MaxFunc(matching, compareKeys)
		before := basesBefore[base.variant]

		pinned := ""
		if before["image"] == image {
			pinned = before["digest"]
		}

		digest, err := fetchDigest(ctx, image, pinned)
		if err != nil {
			return nil, err
		}

		if digest == "" {
			image, digest = before["image"], before["digest"]
		}

		if digest == "" {
			return nil, fmt.Errorf("%s has no image for every architecture", image)
		}

		pins.set(base.variant, "image", image)
		pins.set(base.variant, "digest", digest)
	}

	return pins, nil
}

func fetchDigest(ctx context.Context, image, pinned string) (string, error) {
	head, err := crane.Head(image, dockerHub(ctx)...)
	if err != nil || head.Digest.String() == pinned {
		return pinned, err
	}

	repository, _, _ := strings.Cut(image, ":")

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
		content, err := fetch(ctx, "https://api.github.com/repos/"+tool.repository+"/releases", githubHeaders())
		if err != nil {
			return nil, err
		}

		var releases []githubRelease
		if err := json.Unmarshal(content, &releases); err != nil {
			return nil, fmt.Errorf("parsing %s releases: %w", tool.repository, err)
		}

		pin, err := tool.pin(releases)
		if err != nil {
			return nil, err
		}

		pins[tool.name] = pin
	}

	release, err := fetch(ctx, rustupReleaseURL, nil)
	if err != nil {
		return nil, err
	}

	match := rustupPattern.FindSubmatch(release)
	if match == nil {
		return nil, fmt.Errorf("no rustup version in %s", rustupReleaseURL)
	}

	for _, libc := range []string{"gnu", "musl"} {
		rustup, err := resolveRustup(ctx, string(match[1]), libc)
		if err != nil {
			return nil, err
		}

		pins["rustup-"+libc] = rustup
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

func resolveRustup(ctx context.Context, version, libc string) (map[string]string, error) {
	pin := map[string]string{"version": version}
	for _, architecture := range architectures {
		url := rustupURL + "/archive/" + version + "/" + triples[architecture] + "-unknown-linux-" + libc + "/rustup-init.sha256"

		checksum, err := fetch(ctx, url, nil)
		if err != nil {
			return nil, err
		}

		hash := checksumPattern.FindSubmatch(checksum)
		if hash == nil {
			return nil, fmt.Errorf("no sha256 in %s", url)
		}

		pin[architecture] = "sha256:" + string(hash[1])
	}

	return pin, nil
}

func githubHeaders() map[string]string {
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	return headers
}

func fetch(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", url, response.Status)
	}

	return io.ReadAll(response.Body)
}
