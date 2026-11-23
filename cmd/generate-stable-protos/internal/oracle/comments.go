package oracle

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

// checkComments requires every input comment to appear in the output, except the
// comments inside or attached to a removed declaration or a pruned import, and no other
// comment to appear. The source scan also covers comments without SourceCodeInfo
// locations, such as comments inside option values.
func checkComments(cfg Config, inputTexts, outputTexts map[string]string, input, output *descriptorpb.FileDescriptorSet, removed map[string][][]int32) ([]string, error) {
	outputFiles := map[string]*descriptorpb.FileDescriptorProto{}
	for _, fd := range output.GetFile() {
		outputFiles[fd.GetName()] = fd
	}
	var problems []string
	for _, fd := range input.GetFile() {
		if fd.GetName() == cfg.DraftFile {
			continue
		}
		stable := cfg.stablePath(fd.GetName())
		projected := outputFiles[stable]
		locations := map[string]*descriptorpb.SourceCodeInfo_Location{}
		for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
			if key := fmt.Sprint(loc.GetPath()); locations[key] == nil {
				locations[key] = loc
			}
		}
		gone := slices.Clone(removed[fd.GetName()])
		importIndices := map[int32]int32{}
		for i, dep := range fd.GetDependency() {
			index := slices.Index(projected.GetDependency(), cfg.stablePath(dep))
			if index < 0 {
				gone = append(gone, []int32{fileDependenciesTag, int32(i)})
			}
			importIndices[int32(i)] = int32(index)
		}

		var spans [][]int32
		// surplus counts each comment line in the output minus its count in the input.
		surplus := map[string]int{}
		inputComments := scanComments(inputTexts[fd.GetName()])
		for _, path := range gone {
			loc := locations[fmt.Sprint(path)]
			if loc == nil {
				// Synthetic map entries have no source location.
				continue
			}
			start := comment{line: int(loc.GetSpan()[0]), col: int(loc.GetSpan()[1]), lastCodeCol: -1}
			if slices.ContainsFunc(spans, start.within) {
				// A removed container already covers comments on the declarations inside it.
				continue
			}
			spans = append(spans, loc.GetSpan())
			// Leading comments sit before the span, so the scan below counts them. Trailing
			// comments are left to the scan, including after a closing brace where
			// protoc attaches nothing.
			for _, text := range append(slices.Clone(loc.GetLeadingDetachedComments()), loc.GetLeadingComments()) {
				for _, line := range normalizeComment(text) {
					surplus[line]++
				}
			}
			if path[0] == fileDependenciesTag {
				for _, line := range trailingImportLines(loc, inputComments) {
					surplus[line]++
				}
			}
		}
		for _, c := range inputComments {
			if !slices.ContainsFunc(spans, c.within) {
				for _, line := range commentLines([]comment{c}) {
					surplus[line]--
				}
			}
		}

		text, ok := strings.CutPrefix(outputTexts[stable], cfg.Header)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: missing generated header", stable))
			continue
		}
		for _, line := range commentLines(scanComments(text)) {
			surplus[line]++
		}
		var diffs []string
		for line, n := range surplus {
			if n != 0 {
				diffs = append(diffs, fmt.Sprintf("%+d %q", n, line))
			}
		}
		if len(diffs) > 0 {
			slices.Sort(diffs)
			problems = append(problems, fmt.Sprintf("%s: comment lines differ from the input less removed declarations (+leaked, -lost):\n%s", stable, strings.Join(diffs, "\n")))
			continue
		}
		want := map[string][]string{}
		for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
			if path, kept := remapCommentPath(loc.GetPath(), gone, importIndices); kept {
				recordAttachedComments(want, path, loc, nil)
			}
		}
		got := map[string][]string{}
		header := commentLines(scanComments(cfg.Header))
		for _, loc := range projected.GetSourceCodeInfo().GetLocation() {
			recordAttachedComments(got, loc.GetPath(), loc, header)
		}
		paths := maps.Clone(want)
		maps.Copy(paths, got)
		for _, key := range slices.Sorted(maps.Keys(paths)) {
			if !slices.Equal(want[key], got[key]) {
				diffs = append(diffs, fmt.Sprintf("%s: want %q, got %q", key, want[key], got[key]))
			}
		}
		if len(diffs) > 0 {
			problems = append(problems, fmt.Sprintf("%s: attached comments differ by descriptor path:\n%s", stable, strings.Join(diffs, "\n")))
		}
	}
	return problems, nil
}

// trailingImportLines returns the attached trailing lines outside the import's
// span and last line, which the raw source scan would otherwise count as retained.
func trailingImportLines(loc *descriptorpb.SourceCodeInfo_Location, comments []comment) []string {
	lines := normalizeComment(loc.GetTrailingComments())
	if len(lines) == 0 {
		return nil
	}
	span := loc.GetSpan()
	endLine := span[0]
	if len(span) == 4 {
		endLine = span[2]
	}
	endCol := span[len(span)-1]
	for _, c := range comments {
		if int32(c.line) != endLine || int32(c.col) < endCol || !c.within(span) {
			continue
		}
		for _, line := range commentLines([]comment{c}) {
			if i := slices.Index(lines, line); i >= 0 {
				lines = slices.Delete(lines, i, i+1)
			}
		}
	}
	return lines
}

// remapCommentPath adjusts retained declaration indexes after projection.
func remapCommentPath(path []int32, removed [][]int32, imports map[int32]int32) ([]int32, bool) {
	if len(path) >= 2 && path[0] == fileDependenciesTag {
		index := imports[path[1]]
		if index < 0 {
			return nil, false
		}
		result := slices.Clone(path)
		result[1] = index
		return result, true
	}
	result := slices.Clone(path)
	for _, gone := range removed {
		index := len(gone) - 1
		if index >= len(path) || !slices.Equal(gone[:index], path[:index]) {
			continue
		}
		if gone[index] == path[index] {
			return nil, false
		}
		if gone[index] < path[index] {
			result[index]--
		}
	}
	return result, true
}

func recordAttachedComments(byPath map[string][]string, path []int32, loc *descriptorpb.SourceCodeInfo_Location, header []string) {
	var lines []string
	for _, text := range append(slices.Clone(loc.GetLeadingDetachedComments()), loc.GetLeadingComments(), loc.GetTrailingComments()) {
		lines = append(lines, normalizeComment(text)...)
	}
	// Protoc attaches the generated header to the syntax declaration.
	if len(path) == 1 && path[0] == fileSyntaxTag && len(lines) >= len(header) && slices.Equal(lines[:len(header)], header) {
		lines = lines[len(header):]
	}
	if len(lines) > 0 {
		key := fmt.Sprint(path)
		byPath[key] = append(byPath[key], lines...)
	}
}

type comment struct {
	line, col, lastCodeCol int // Columns are zero-based, with tabs advancing to a multiple of 8.
	text                   string
}

// within reports whether c starts inside a SourceCodeInfo span or trails it without intervening code.
func (c comment) within(span []int32) bool {
	startLine, startCol, endLine, endCol := span[0], span[1], span[0], span[2]
	if len(span) == 4 {
		endLine, endCol = span[2], span[3]
	}
	pos := []int32{int32(c.line), int32(c.col)}
	return slices.Compare(pos, []int32{startLine, startCol}) >= 0 &&
		(slices.Compare(pos, []int32{endLine, endCol}) < 0 ||
			pos[0] == endLine && c.lastCodeCol >= 0 && int32(c.lastCodeCol) < endCol)
}

// scanComments returns the comments in proto source, skipping string literals.
func scanComments(src string) []comment {
	var comments []comment
	line, col, lastCodeCol := 0, 0, -1
	advance := func(n int, i int) int {
		for _, b := range []byte(src[i : i+n]) {
			switch b {
			case '\n':
				line, col, lastCodeCol = line+1, 0, -1
			case '\t':
				col += 8 - col%8
			default:
				col++
			}
		}
		return i + n
	}
	for i := 0; i < len(src); {
		rest := src[i:]
		switch {
		case rest[0] == '"' || rest[0] == '\'':
			n := 1
			for n < len(rest) && rest[n] != rest[0] && rest[n] != '\n' {
				if rest[n] == '\\' {
					n++
				}
				n++
			}
			i = advance(min(n+1, len(rest)), i)
			lastCodeCol = col - 1
		case strings.HasPrefix(rest, "//"), strings.HasPrefix(rest, "/*"):
			end := len(rest)
			if rest[1] == '/' {
				if n := strings.IndexByte(rest, '\n'); n >= 0 {
					end = n
				}
			} else if n := strings.Index(rest[2:], "*/"); n >= 0 {
				end = n + 4
			}
			comments = append(comments, comment{line, col, lastCodeCol, rest[:end]})
			i = advance(end, i)
		default:
			if !strings.ContainsRune(" \t\r\n\v\f", rune(rest[0])) {
				lastCodeCol = col
			}
			i = advance(1, i)
		}
	}
	return comments
}

// commentLines strips the comment delimiters and normalizes each line.
func commentLines(comments []comment) []string {
	var lines []string
	for _, c := range comments {
		text, block := strings.CutPrefix(c.text, "/*")
		if block {
			text = strings.TrimSuffix(text, "*/")
		} else {
			text = strings.TrimPrefix(text, "//")
		}
		lines = append(lines, normalizeComment(text)...)
	}
	return lines
}

// normalizeComment splits comment text into trimmed, nonempty lines without the leading
// "*" of block comment margins, matching both raw comments and protoc's stripped ones.
func normalizeComment(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
