package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile/experimental/parser"
	"github.com/bufbuild/protocompile/experimental/report"
	"github.com/bufbuild/protocompile/experimental/source"
	"github.com/bufbuild/protocompile/experimental/token"
)

// FuzzRewriteImportPaths checks rewriteImportPaths on arbitrary source: whenever it
// accepts the input, every token outside an import path is unchanged, and each import
// path, joined across concatenated literals, only loses the _next of a leading
// temporal/api_next/. The import forms the fixtures exercise end to end are the seeds.
func FuzzRewriteImportPaths(f *testing.F) {
	for _, seed := range []string{
		`syntax = "proto3";` + "\n" + `import "temporal/api_next/used.proto";`,
		`import public /* before */ 'temporal/api_next/used.proto' /* after */;`,
		`import "temporal/api_n" /* internal comment */ "ext/another.proto";`,
		`import "temporal/api_next" "/split.proto";`,
		`import "google/protobuf/any.proto";`,
		`import weak "temporal/api_next/\x41.proto";`,
		`option java_package = "temporal/api_next/used.proto";` + "\n" + `// import "temporal/api_next/used.proto";`,
		`import "temporal/api_next/";`,
		`import "temporal/api_nextfoo.proto";`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		output, err := testConfig.rewriteImportPaths(input)
		if err != nil {
			return
		}
		in, ok := splitImportPaths(input)
		if !ok {
			t.Fatalf("accepted input that does not parse: %q", input)
		}
		out, ok := splitImportPaths(output)
		if !ok {
			t.Fatalf("output does not parse\ninput:  %q\noutput: %q", input, output)
		}
		if !slices.Equal(in.other, out.other) {
			t.Fatalf("tokens outside import paths changed\ninput:  %q\noutput: %q", input, output)
		}
		if len(in.paths) != len(out.paths) {
			t.Fatalf("import count changed from %d to %d\ninput:  %q\noutput: %q", len(in.paths), len(out.paths), input, output)
		}
		for i, path := range in.paths {
			if rest, ok := strings.CutPrefix(path, oracleConfig.NextPrefix); ok {
				path = oracleConfig.StablePrefix + rest
			}
			if out.paths[i] != path {
				t.Fatalf("import %d: want %q, got %q\ninput:  %q\noutput: %q", i, path, out.paths[i], input, output)
			}
		}
	})
}

type importPaths struct {
	// other holds the text of every token outside an import path, in order.
	other []string
	// paths holds each import's decoded path, joined across concatenated literals.
	paths []string
}

// splitImportPaths separates the import paths in text from everything else, and reports
// whether text parses without errors.
func splitImportPaths(text string) (importPaths, bool) {
	var diagnostics report.Report
	file, _ := parser.Parse("fuzz.proto", source.NewFile("fuzz.proto", text), &diagnostics)
	if _, hasErrors := errorDiagnostics(&diagnostics); hasErrors {
		return importPaths{}, false
	}
	var spans []source.Span
	for imp := range file.Imports() {
		spans = append(spans, imp.ImportPath().Span())
	}
	result := importPaths{paths: make([]string, len(spans))}
	seen := make([]bool, len(spans))
	for tok := range file.Stream().All() {
		span := tok.LeafSpan()
		i := slices.IndexFunc(spans, func(s source.Span) bool { return s.Start <= span.Start && span.End <= s.End })
		if i < 0 || tok.Kind() != token.String {
			result.other = append(result.other, span.Text())
			continue
		}
		// The first literal of a concatenation carries the joined, unescaped value.
		if !seen[i] {
			result.paths[i], seen[i] = tok.AsString().Text(), true
		}
	}
	return result, true
}
