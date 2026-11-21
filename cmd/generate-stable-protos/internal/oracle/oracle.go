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
	"google.golang.org/protobuf/reflect/protoreflect"
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
	// Header is the text the generator prepends to every output file.
	Header string
}

// DeclarationChange identifies a declaration added to or removed from the expected projection.
type DeclarationChange struct {
	Kind string `json:"kind"`
	File string `json:"file"`
	Path string `json:"path"`
}

// Result contains the oracle's diagnostics and its structured declaration changes.
type Result struct {
	Problems           []string
	DescriptorDiff     string
	DeclarationChanges []DeclarationChange
}

// AllProblems returns the diagnostics, including any descriptor diff.
func (r Result) AllProblems() []string {
	if r.DescriptorDiff == "" {
		return r.Problems
	}
	return append(slices.Clone(r.Problems), r.DescriptorDiff)
}

// Check compares the stable tree under outputRoot with the api_next tree under
// inputRoot and returns every deviation from the projection. The error reports a
// failure to run the check itself.
func Check(cfg Config, inputRoot, outputRoot string) (Result, error) {
	inputTexts, err := readTree(inputRoot, cfg.NextPrefix)
	if err != nil {
		return Result{}, err
	}
	outputTexts, err := readTree(outputRoot, cfg.StablePrefix)
	if err != nil {
		return Result{}, err
	}
	input, _, err := compile([]string{inputRoot}, slices.Sorted(maps.Keys(inputTexts)))
	if err != nil {
		return Result{}, fmt.Errorf("compile input: %w", err)
	}
	// The output directory comes first so it shadows any stable tree checked in under inputRoot.
	output, warnings, err := compile([]string{outputRoot, inputRoot}, slices.Sorted(maps.Keys(outputTexts)))
	if err != nil {
		return Result{}, fmt.Errorf("compile output: %w", err)
	}
	var problems []string
	if strings.Contains(warnings, "is unused") {
		problems = append(problems, fmt.Sprintf("stable output has unused imports:\n%s", warnings))
	}

	p, err := newProjector(cfg, input)
	if err != nil {
		return Result{}, err
	}
	expected := map[string]*descriptorpb.FileDescriptorProto{}
	removed := map[string][][]int32{}
	inputDeps := map[string]bool{}
	for _, fd := range input.GetFile() {
		if fd.GetName() == cfg.DraftFile {
			continue
		}
		projected, paths := p.projectFile(fd)
		expected[projected.GetName()] = projected
		removed[fd.GetName()] = paths
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
		// Comment differences would only repeat the declarations that differ.
		return Result{
			Problems:           problems,
			DescriptorDiff:     fmt.Sprintf("stable descriptors differ from the oracle projection (-expected +actual):\n%s", diff),
			DeclarationChanges: compareDeclarations(expected, actual),
		}, nil
	}
	commentProblems, err := checkComments(cfg, inputTexts, outputTexts, input, output, removed)
	if err != nil {
		return Result{}, err
	}
	return Result{Problems: append(problems, commentProblems...)}, nil
}

func compareDeclarations(expected, actual map[string]*descriptorpb.FileDescriptorProto) []DeclarationChange {
	var changes []DeclarationChange
	for _, side := range []struct {
		kind           string
		files, against map[string]*descriptorpb.FileDescriptorProto
	}{
		{"added", actual, expected},
		{"removed", expected, actual},
	} {
		for _, file := range slices.Sorted(maps.Keys(side.files)) {
			paths, against := map[string]bool{}, map[string]bool{}
			collectDeclarationPaths(side.files[file].ProtoReflect(), "", paths)
			if fd := side.against[file]; fd != nil {
				collectDeclarationPaths(fd.ProtoReflect(), "", against)
			}
			for _, path := range slices.Sorted(maps.Keys(paths)) {
				if !against[path] {
					changes = append(changes, DeclarationChange{Kind: side.kind, File: file, Path: path})
				}
			}
		}
	}
	return changes
}

// collectDeclarationPaths walks named descriptor children, using names instead of list indexes.
func collectDeclarationPaths(message protoreflect.Message, prefix string, paths map[string]bool) {
	message.Range(func(field protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if !field.IsList() || field.Message() == nil {
			return true
		}
		name := field.Message().Fields().ByName("name")
		if name == nil {
			return true
		}
		list := v.List()
		for i := range list.Len() {
			child := list.Get(i).Message()
			path := prefix + string(field.Name()) + "." + child.Get(name).String()
			paths[path] = true
			collectDeclarationPaths(child, path+".", paths)
		}
		return true
	})
}

func (cfg Config) stablePath(path string) string {
	if rest, ok := strings.CutPrefix(path, cfg.NextPrefix); ok {
		return cfg.StablePrefix + rest
	}
	return path
}
