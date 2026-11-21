// Package oracle checks the output of generate-stable-protos against an independent
// reference: protoc compiles the api_next input and the stable output, and the stable
// descriptors must equal the input descriptors with the draft declarations projected
// out. The projection works on descriptors, where removing a declaration is simple, so
// it shares no logic with the generator's AST editing.
package oracle

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Config describes the layout of the generator's input and output.
type Config struct {
	// NextPrefix is the path prefix of the api_next input, such as "temporal/api_next/".
	NextPrefix string
	// StablePrefix is the path prefix of the stable output, such as "temporal/api/".
	StablePrefix string
	// DraftFile is the input path of the file that defines the draft options.
	DraftFile string
}

// Check compares the stable tree under outputRoot with the api_next tree under
// inputRoot and returns every deviation from the projection. The error reports a
// failure to run the check itself.
func Check(cfg Config, inputRoot, outputRoot string) ([]string, error) {
	inputTexts, err := readTree(inputRoot, cfg.NextPrefix)
	if err != nil {
		return nil, err
	}
	outputTexts, err := readTree(outputRoot, cfg.StablePrefix)
	if err != nil {
		return nil, err
	}
	input, _, err := compile([]string{inputRoot}, slices.Sorted(maps.Keys(inputTexts)))
	if err != nil {
		return nil, fmt.Errorf("compile input: %w", err)
	}
	// The output directory comes first so it shadows any stable tree checked in under inputRoot.
	output, warnings, err := compile([]string{outputRoot, inputRoot}, slices.Sorted(maps.Keys(outputTexts)))
	if err != nil {
		return nil, fmt.Errorf("compile output: %w", err)
	}
	var problems []string
	if strings.Contains(warnings, "is unused") {
		problems = append(problems, fmt.Sprintf("stable output has unused imports:\n%s", warnings))
	}

	p, err := newProjector(cfg, input)
	if err != nil {
		return nil, err
	}
	expected := map[string]*descriptorpb.FileDescriptorProto{}
	inputDeps := map[string]bool{}
	for _, fd := range input.GetFile() {
		if fd.GetName() == cfg.DraftFile {
			continue
		}
		projected, _ := p.projectFile(fd)
		expected[projected.GetName()] = projected
		for _, dep := range fd.GetDependency() {
			inputDeps[cfg.stablePath(dep)] = true
		}
	}
	actual := map[string]*descriptorpb.FileDescriptorProto{}
	for _, fd := range output.GetFile() {
		// Import pruning needs cross-file usage analysis, which would re-implement the
		// generator; the unused-import warning check above covers it instead.
		for _, dep := range fd.GetDependency() {
			if !inputDeps[dep] {
				problems = append(problems, fmt.Sprintf("%s: import %s is not an import of the input", fd.GetName(), dep))
			}
		}
		actual[fd.GetName()] = normalize(fd)
	}
	if diff := cmp.Diff(expected, actual, protocmp.Transform()); diff != "" {
		return append(problems, fmt.Sprintf("stable descriptors differ from the oracle projection (-expected +actual):\n%s", diff)), nil
	}
	return problems, nil
}

func (cfg Config) stablePath(path string) string {
	if rest, ok := strings.CutPrefix(path, cfg.NextPrefix); ok {
		return cfg.StablePrefix + rest
	}
	return path
}
