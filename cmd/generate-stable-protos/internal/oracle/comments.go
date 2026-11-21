package oracle

import (
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/types/descriptorpb"
)

// checkComments requires every input comment to appear in the output, except the
// comments inside or attached to a removed declaration or a pruned import, and no other
// comment to appear. Comments are scanned from the source text rather than taken from
// SourceCodeInfo, which has no place for a comment inside an option value.
func checkComments(cfg Config, inputTexts, outputTexts map[string]string, input, output *descriptorpb.FileDescriptorSet, removed map[string][][]int32) ([]string, error) {
	outputDeps := map[string][]string{}
	for _, fd := range output.GetFile() {
		outputDeps[fd.GetName()] = fd.GetDependency()
	}
	var problems []string
	for _, fd := range input.GetFile() {
		if fd.GetName() == cfg.DraftFile {
			continue
		}
		stable := cfg.stablePath(fd.GetName())
		locations := map[string]*descriptorpb.SourceCodeInfo_Location{}
		for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
			if key := fmt.Sprint(loc.GetPath()); locations[key] == nil {
				locations[key] = loc
			}
		}
		gone := removed[fd.GetName()]
		for i, dep := range fd.GetDependency() {
			if !slices.Contains(outputDeps[stable], cfg.stablePath(dep)) {
				gone = append(gone, []int32{fileDependenciesTag, int32(i)})
			}
		}

		var spans [][]int32
		// surplus counts each comment line in the output minus its count in the input.
		surplus := map[string]int{}
		for _, path := range gone {
			loc := locations[fmt.Sprint(path)]
			if loc == nil {
				return nil, fmt.Errorf("%s: no source location for removed path %v", fd.GetName(), path)
			}
			spans = append(spans, loc.GetSpan())
			// Leading comments sit before the span, so the scan below counts them. Trailing
			// comments are left to the scan: one on the span's last line counts as part of
			// the declaration, even after a closing brace, where protoc attaches nothing.
			for _, text := range append(slices.Clone(loc.GetLeadingDetachedComments()), loc.GetLeadingComments()) {
				for _, line := range normalizeComment(text) {
					surplus[line]++
				}
			}
		}
		for _, c := range scanComments(inputTexts[fd.GetName()]) {
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
		}
	}
	return problems, nil
}

type comment struct {
	line, col int // zero-based, columns counted like protoc: tabs advance to a multiple of 8
	text      string
}

// within reports whether c starts inside a SourceCodeInfo span or later on its last line.
func (c comment) within(span []int32) bool {
	startLine, startCol, endLine, endCol := span[0], span[1], span[0], span[2]
	if len(span) == 4 {
		endLine, endCol = span[2], span[3]
	}
	pos := []int32{int32(c.line), int32(c.col)}
	return slices.Compare(pos, []int32{startLine, startCol}) >= 0 && (slices.Compare(pos, []int32{endLine, endCol}) < 0 || pos[0] == endLine)
}

// scanComments returns the comments in proto source, skipping string literals.
func scanComments(src string) []comment {
	var comments []comment
	line, col := 0, 0
	advance := func(n int, i int) int {
		for _, b := range []byte(src[i : i+n]) {
			switch b {
			case '\n':
				line, col = line+1, 0
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
		case strings.HasPrefix(rest, "//"), strings.HasPrefix(rest, "/*"):
			end := len(rest)
			if rest[1] == '/' {
				if n := strings.IndexByte(rest, '\n'); n >= 0 {
					end = n
				}
			} else if n := strings.Index(rest[2:], "*/"); n >= 0 {
				end = n + 4
			}
			comments = append(comments, comment{line, col, rest[:end]})
			i = advance(end, i)
		default:
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
