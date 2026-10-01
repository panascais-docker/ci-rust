package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	digestsDirectory = "digests"
	defaultVariant   = "alpine"
)

type registry struct {
	host    string
	image   string
	secrets string
}

var registries = []registry{
	{host: "ghcr.io", image: "ghcr.io/panascais-docker/ci-rust/ci-rust", secrets: "CONTAINER"},
	{image: "panascais/ci-rust", secrets: "DOCKER"},
	{host: "quay.io", image: "quay.io/panascais/ci-rust", secrets: "QUAY"},
}

type build struct {
	line    string
	variant string
	base    string
	version string
	names   []string
}

type bakeFile struct {
	Group  map[string]bakeGroup  `json:"group"`
	Target map[string]bakeTarget `json:"target"`
}

type bakeGroup struct {
	Targets []string `json:"targets"`
}

type bakeTarget struct {
	Args      map[string]string `json:"args"`
	Context   string            `json:"context"`
	Output    []string          `json:"output,omitzero"`
	Platforms []string          `json:"platforms"`
	Tags      []string          `json:"tags,omitzero"`
	Target    string            `json:"target"`
}

type export func(build build) (tags, outputs []string)

func buildCommand() *cobra.Command {
	var platform string

	command := &cobra.Command{
		Use:   "build <line>",
		Short: "Smoke test and build every variant of a line, pushing by digest on GitHub Actions and loading locally",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, arguments []string) error { return buildLine(arguments[0], platform) },
	}
	command.Flags().StringVar(&platform, "platform", "linux/"+runtime.GOARCH, "platform to smoke test and build")

	return command
}

func buildLine(line, platform string) error {
	lines, err := readConfiguration(linesFile)
	if err != nil {
		return err
	}

	basePins, err := readConfiguration(basesFile)
	if err != nil {
		return err
	}

	toolPins, err := readConfiguration(toolsFile)
	if err != nil {
		return err
	}

	builds, err := planBuilds(line, lines, basePins)
	if err != nil {
		return err
	}

	pushing := os.Getenv("GITHUB_ACTIONS") == "true"

	revision := "local"
	if pushing {
		if revision, err = output("git", "rev-parse", "--short", "HEAD"); err != nil {
			return err
		}

		for _, registry := range registries {
			if err := registry.login(); err != nil {
				return err
			}
		}
	}

	arguments := buildArguments(toolPins, revision)

	smoke := func(build) ([]string, []string) { return nil, []string{"type=cacheonly"} }
	if err := bake(builds, arguments, platform, "smoke", smoke); err != nil {
		return err
	}

	if !pushing {
		load := func(build build) ([]string, []string) { return build.tags(registries...), nil }

		return bake(builds, arguments, platform, "image", load, "--load")
	}

	return push(builds, arguments, platform)
}

func planBuilds(line string, lines, basePins configuration) ([]build, error) {
	if _, found := lines[line]; !found {
		return nil, fmt.Errorf("invalid line %q, expected one of %s", line, strings.Join(sortedKeys(lines), ", "))
	}

	version := lines[line]["version"]
	if !regexp.MustCompile(`^` + regexp.QuoteMeta(line) + `\.\d+$`).MatchString(version) {
		return nil, fmt.Errorf("invalid rust version %q for %s", version, line)
	}

	var builds []build
	for _, variant := range sortedKeys(basePins) {
		image, digest := basePins[variant]["image"], basePins[variant]["digest"]
		if image == "" || !digestPattern.MatchString(digest) {
			return nil, fmt.Errorf("invalid %s base %q@%q", variant, image, digest)
		}

		builds = append(builds, build{
			line:    line,
			variant: variant,
			base:    image + "@" + digest,
			version: version,
			names:   resolveNames(sortedKeys(lines), line, variant, version),
		})
	}

	return builds, nil
}

func resolveNames(lines []string, line, variant, version string) []string {
	names := aliases(lines, line, version, "-"+variant)
	if variant == defaultVariant {
		names = append(names, aliases(lines, line, version, "")...)
	}

	return names
}

func aliases(lines []string, line, version, suffix string) []string {
	major, _, _ := strings.Cut(line, ".")
	sameMajor := where(lines, func(candidate string) bool { return strings.HasPrefix(candidate, major+".") })

	names := []string{line + suffix, version + suffix}
	if slices.MaxFunc(sameMajor, compareKeys) == line {
		names = append(names, major+suffix)
	}

	if slices.MaxFunc(lines, compareKeys) == line {
		names = append(names, "latest"+suffix)
	}

	return names
}

func where(lines []string, keep func(string) bool) []string {
	return slices.DeleteFunc(slices.Clone(lines), func(line string) bool { return !keep(line) })
}

func buildArguments(toolPins configuration, revision string) map[string]string {
	arguments := map[string]string{
		"BUILD_DATE": time.Now().UTC().Format(time.RFC3339),
		"VCS_REF":    revision,
	}

	for name, pin := range toolPins {
		prefix := argumentPrefix(name)
		arguments[prefix+"_VERSION"] = pin["version"]
		for _, architecture := range architectures {
			arguments[prefix+"_SHA256_"+strings.ToUpper(architecture)] = pin[architecture]
		}
	}

	return arguments
}

func description(build build, arguments map[string]string) string {
	versions := []string{"rust " + build.version}
	for _, tool := range tools {
		versions = append(versions, tool.name+" "+arguments[argumentPrefix(tool.name)+"_VERSION"])
	}

	return "Panascais Rust Continuous Integration Image (" + strings.Join(versions, ", ") + ")"
}

func argumentPrefix(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

func (build build) target() string {
	return strings.ReplaceAll(build.line, ".", "_") + "-" + build.variant
}

func (build build) system() string {
	if build.variant == "alpine" {
		return "alpine"
	}

	return "debian"
}

func (build build) tags(registries ...registry) []string {
	var tags []string
	for _, registry := range registries {
		for _, name := range build.names {
			tags = append(tags, registry.image+":"+name)
		}
	}

	return tags
}

func push(builds []build, arguments map[string]string, platform string) error {
	var images []string
	for _, registry := range registries {
		images = append(images, registry.image)
	}

	metadataFile := filepath.Join(os.TempDir(), "ci-rust-metadata.json")
	push := func(build) ([]string, []string) {
		return nil, []string{`type=image,"name=` + strings.Join(images, ",") + `",push-by-digest=true,name-canonical=true,push=true`}
	}
	if err := bake(builds, arguments, platform, "image", push, "--metadata-file", metadataFile); err != nil {
		return err
	}

	pushed, err := readPushedDigests(metadataFile, builds)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(digestsDirectory, 0o755); err != nil {
		return err
	}

	_, architecture, _ := strings.Cut(platform, "/")

	return writeConfiguration(filepath.Join(digestsDirectory, builds[0].line+"-"+architecture+".json"), pushed)
}

func readPushedDigests(metadataFile string, builds []build) (configuration, error) {
	content, err := os.ReadFile(metadataFile)
	if err != nil {
		return nil, err
	}

	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(content, &metadata); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", metadataFile, err)
	}

	pushed := configuration{}
	for _, build := range builds {
		var result struct {
			Digest string `json:"containerimage.digest"`
		}
		if err := json.Unmarshal(metadata[build.target()], &result); err != nil || !digestPattern.MatchString(result.Digest) {
			return nil, fmt.Errorf("no pushed digest for %s in %s", build.target(), metadataFile)
		}

		pushed.set(build.line, build.variant, result.Digest)
	}

	return pushed, nil
}

func bake(builds []build, arguments map[string]string, platform, stage string, export export, flags ...string) error {
	definition, err := bakeDefinition(builds, arguments, platform, stage, export)
	if err != nil {
		return err
	}

	process := command("docker", slices.Concat([]string{"buildx", "bake", "--file", "-", "--progress=plain"}, flags)...)
	process.Stdin = bytes.NewReader(definition)

	return process.Run()
}

func bakeDefinition(builds []build, arguments map[string]string, platform, stage string, export export) ([]byte, error) {
	targets := make(map[string]bakeTarget, len(builds))
	for _, build := range builds {
		args := maps.Clone(arguments)
		args["DESCRIPTION"] = description(build, arguments)
		args["BASE_IMAGE"] = build.base
		args["RUST_VERSION"] = build.version
		args["SYSTEM"] = build.system()

		tags, outputs := export(build)
		targets[build.target()] = bakeTarget{
			Args:      args,
			Context:   ".",
			Output:    outputs,
			Platforms: []string{platform},
			Tags:      tags,
			Target:    stage,
		}
	}

	return json.Marshal(bakeFile{
		Group:  map[string]bakeGroup{"default": {Targets: slices.Sorted(maps.Keys(targets))}},
		Target: targets,
	})
}

func (registry registry) login() error {
	arguments := []string{"login"}
	if registry.host != "" {
		arguments = append(arguments, registry.host)
	}

	login := command("docker", append(arguments, "-u", os.Getenv(registry.secrets+"_REGISTRY_USERNAME"), "--password-stdin")...)
	login.Stdin = strings.NewReader(os.Getenv(registry.secrets + "_REGISTRY_TOKEN"))

	return login.Run()
}

func command(name string, arguments ...string) *exec.Cmd {
	process := exec.Command(name, arguments...)
	process.Stdout, process.Stderr = os.Stdout, os.Stderr

	return process
}

func output(name string, arguments ...string) (string, error) {
	process := exec.Command(name, arguments...)
	process.Stderr = os.Stderr

	result, err := process.Output()

	return strings.TrimSpace(string(result)), err
}
