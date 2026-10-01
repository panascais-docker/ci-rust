package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
)

const (
	basesFile = "configuration/bases.json"
	linesFile = "configuration/lines.json"
	toolsFile = "configuration/tools.json"
)

type configuration map[string]map[string]string

func (value configuration) set(line, variant, entry string) {
	if value[line] == nil {
		value[line] = map[string]string{}
	}

	value[line][variant] = entry
}

func (value configuration) equal(other configuration) bool {
	return maps.EqualFunc(value, other, maps.Equal)
}

func (value configuration) changed(before configuration, line string) bool {
	return !maps.Equal(value[line], before[line])
}

func readConfiguration(path string) (configuration, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var value configuration
	if err := json.Unmarshal(content, &value); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return value, nil
}

func writeConfiguration(path string, value configuration) error {
	var lines []string
	for _, line := range sortedKeys(value) {
		var variants []string
		for _, variant := range sortedKeys(value[line]) {
			variants = append(variants, fmt.Sprintf("%q:%q", variant, value[line][variant]))
		}

		lines = append(lines, fmt.Sprintf("%q:{%s}", line, strings.Join(variants, ",")))
	}

	var indented bytes.Buffer
	if err := json.Indent(&indented, []byte("{"+strings.Join(lines, ",")+"}"), "", "    "); err != nil {
		return err
	}

	indented.WriteByte('\n')

	return os.WriteFile(path, indented.Bytes(), 0o644)
}

func sortedKeys[Value any](record map[string]Value) []string {
	return slices.SortedFunc(maps.Keys(record), compareKeys)
}

func compareKeys(left, right string) int {
	return cmp.Or(slices.Compare(keyNumbers(left), keyNumbers(right)), strings.Compare(left, right))
}

func keyNumbers(key string) []int {
	var numbers []int
	for part := range strings.SplitSeq(key, ".") {
		number, _ := strconv.Atoi(part)
		numbers = append(numbers, number)
	}

	return numbers
}
