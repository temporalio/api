package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const draftFieldNumber = 77001

var (
	root   = findRepoRoot()
	source = filepath.Join(root, "temporal", "api_next")
	target = filepath.Join(root, "temporal", "api")

	apiNextImportRe = regexp.MustCompile(`"temporal/api_next/([^"]+)\.proto"`)
	apiNextGoPkgRe  = regexp.MustCompile(`"github\.com/temporalio/api-go/api_next/([^"]+)"`)
)

type removeSpan struct {
	startLine int
	endLine   int
}

type stripPlan map[string][]removeSpan

func main() {
	if _, err := os.Stat(source); err != nil {
		fatalf("missing source proto tree: %s", source)
	}
	plan, err := buildStripPlan()
	if err != nil {
		fatalf("build strip plan: %v", err)
	}
	if err := os.RemoveAll(target); err != nil {
		fatalf("remove target: %v", err)
	}
	if err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		return copyStableFile(plan, path, d, err)
	}); err != nil {
		fatalf("generate stable API: %v", err)
	}
}

func buildStripPlan() (stripPlan, error) {
	cmd := exec.Command("buf", "build", "--config", "buf.next.yaml", "-o", "-")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var stderr bytes.Buffer
		if ee, ok := err.(*exec.ExitError); ok {
			stderr.Write(ee.Stderr)
		}
		return nil, fmt.Errorf("buf build: %w%s", err, stderr.String())
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(out, &set); err != nil {
		return nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}

	plan := make(stripPlan)
	for _, file := range set.GetFile() {
		if !strings.HasPrefix(file.GetName(), "temporal/api_next/") {
			continue
		}
		locations := sourceLocations(file)
		collectFileStrips(plan, file, locations)
	}
	return plan, nil
}

func collectFileStrips(plan stripPlan, file *descriptorpb.FileDescriptorProto, locations map[string]*descriptorpb.SourceCodeInfo_Location) {
	name := file.GetName()
	for i, msg := range file.GetMessageType() {
		collectMessageStrips(plan, name, locations, []int32{4, int32(i)}, msg)
	}
	for i, enum := range file.GetEnumType() {
		path := []int32{5, int32(i)}
		if draftOptionValue(enum.GetOptions()) != "" {
			plan.add(name, locations, path)
			continue
		}
		for j, value := range enum.GetValue() {
			if draftOptionValue(value.GetOptions()) != "" {
				plan.add(name, locations, append(slices.Clone(path), 2, int32(j)))
			}
		}
	}
	for i, service := range file.GetService() {
		path := []int32{6, int32(i)}
		if draftOptionValue(service.GetOptions()) != "" {
			plan.add(name, locations, path)
			continue
		}
		for j, method := range service.GetMethod() {
			if draftOptionValue(method.GetOptions()) != "" {
				plan.add(name, locations, append(slices.Clone(path), 2, int32(j)))
			}
		}
	}
}

func collectMessageStrips(plan stripPlan, file string, locations map[string]*descriptorpb.SourceCodeInfo_Location, path []int32, msg *descriptorpb.DescriptorProto) {
	if draftOptionValue(msg.GetOptions()) != "" {
		plan.add(file, locations, path)
		return
	}
	for i, field := range msg.GetField() {
		if draftOptionValue(field.GetOptions()) != "" {
			plan.add(file, locations, append(slices.Clone(path), 2, int32(i)))
		}
	}
	for i, nested := range msg.GetNestedType() {
		collectMessageStrips(plan, file, locations, append(slices.Clone(path), 3, int32(i)), nested)
	}
	for i, enum := range msg.GetEnumType() {
		enumPath := append(slices.Clone(path), 4, int32(i))
		if draftOptionValue(enum.GetOptions()) != "" {
			plan.add(file, locations, enumPath)
			continue
		}
		for j, value := range enum.GetValue() {
			if draftOptionValue(value.GetOptions()) != "" {
				plan.add(file, locations, append(slices.Clone(enumPath), 2, int32(j)))
			}
		}
	}
}

func (p stripPlan) add(file string, locations map[string]*descriptorpb.SourceCodeInfo_Location, path []int32) {
	location := locations[pathKey(path)]
	if location == nil || len(location.GetSpan()) < 3 {
		return
	}
	span := location.GetSpan()
	endLine := int(span[0])
	if len(span) >= 4 {
		endLine = int(span[2])
	}
	p[file] = append(p[file], removeSpan{
		startLine: int(span[0]),
		endLine:   endLine,
	})
}

func sourceLocations(file *descriptorpb.FileDescriptorProto) map[string]*descriptorpb.SourceCodeInfo_Location {
	locations := make(map[string]*descriptorpb.SourceCodeInfo_Location)
	for _, location := range file.GetSourceCodeInfo().GetLocation() {
		locations[pathKey(location.GetPath())] = location
	}
	return locations
}

func pathKey(path []int32) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = fmt.Sprint(p)
	}
	return strings.Join(parts, ".")
}

func draftOptionValue(opts proto.Message) string {
	if opts == nil {
		return ""
	}
	unknown := opts.ProtoReflect().GetUnknown()
	for len(unknown) > 0 {
		num, typ, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return ""
		}
		unknown = unknown[n:]
		if num == draftFieldNumber && typ == protowire.BytesType {
			val, n := protowire.ConsumeBytes(unknown)
			if n < 0 {
				return ""
			}
			return string(val)
		}
		n = protowire.ConsumeFieldValue(num, typ, unknown)
		if n < 0 {
			return ""
		}
		unknown = unknown[n:]
	}
	return ""
}

func copyStableFile(plan stripPlan, path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(source, path)
	if err != nil {
		return err
	}
	if filepath.ToSlash(rel) == "draft.proto" {
		return nil
	}

	out := filepath.Join(target, rel)
	if d.IsDir() {
		return os.MkdirAll(out, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if filepath.Ext(path) != ".proto" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fileName := filepath.ToSlash(filepath.Join("temporal", "api_next", rel))
	return os.WriteFile(out, []byte(stableProto(string(data), plan[fileName])), 0o644)
}

func stableProto(content string, removals []removeSpan) string {
	lines := splitAfter(content)
	lines = removeDraftSpans(lines, removals)
	lines = rewriteStableImports(lines)
	lines = addGeneratedHeader(lines)
	return strings.Join(lines, "")
}

func removeDraftSpans(lines []string, removals []removeSpan) []string {
	if len(removals) == 0 {
		return lines
	}
	slices.SortFunc(removals, func(a, b removeSpan) int {
		if a.startLine != b.startLine {
			return a.startLine - b.startLine
		}
		return a.endLine - b.endLine
	})

	removeLines := make(map[int]struct{})
	for _, span := range removals {
		start := span.startLine
		for start > 0 && isCommentLine(lines[start-1]) {
			start--
		}
		for line := start; line <= span.endLine && line < len(lines); line++ {
			removeLines[line] = struct{}{}
		}
	}

	out := make([]string, 0, len(lines)-len(removeLines))
	for i, line := range lines {
		if _, remove := removeLines[i]; remove {
			continue
		}
		out = append(out, line)
	}
	return cleanupStrippedBlankLines(out)
}

func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "//")
}

func rewriteStableImports(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == `import "temporal/api_next/draft.proto";` {
			continue
		}
		line = apiNextImportRe.ReplaceAllString(line, `"temporal/api/${1}.proto"`)
		line = apiNextGoPkgRe.ReplaceAllString(line, `"go.temporal.io/api/${1}"`)
		out = append(out, line)
	}
	return out
}

func addGeneratedHeader(lines []string) []string {
	header := []string{
		"// Code generated by cmd/generate-stable-api. DO NOT EDIT.\n",
		"// Source: temporal/api_next\n",
		"\n",
	}
	return append(header, lines...)
}

func cleanupStrippedBlankLines(lines []string) []string {
	cleaned := make([]string, 0, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			cleaned = append(cleaned, line)
			continue
		}
		previousBlank := len(cleaned) > 0 && strings.TrimSpace(cleaned[len(cleaned)-1]) == ""
		nextText := nextNonBlank(lines[i+1:])
		if previousBlank && (nextText == "" || strings.HasPrefix(nextText, "//") || strings.HasPrefix(nextText, "option ")) {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return cleaned
}

func splitAfter(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.SplitAfter(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func nextNonBlank(lines []string) string {
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func findRepoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		fatalf("get working directory: %v", err)
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "buf.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fatalf("could not find api repo root")
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
