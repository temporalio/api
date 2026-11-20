package oracle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testConfig = Config{
	NextPrefix:   "temporal/api_next/",
	StablePrefix: "temporal/api/",
	DraftFile:    "temporal/api_next/protometa/v1/draft.proto",
	Header:       "// Code generated. DO NOT EDIT.\n\n",
}

// TestCheck feeds Check the correct stable output for testdata/input, then that output
// with one fault injected, and requires Check to accept the first and report each fault.
// A lenient oracle would otherwise let every generator fixture pass unnoticed.
func TestCheck(t *testing.T) {
	t.Parallel()
	const path = "temporal/api/fixture.proto"
	correct, err := os.ReadFile(filepath.Join("testdata/output", path))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, old, new string
		// want are substrings that one reported problem must all contain.
		want []string
	}{
		{name: "correct output"},
		{
			name: "lost stable field",
			old:  "  StableChild stable_child = 3; // trailing stable_child\n",
			want: []string{"stable descriptors differ", "stable_child"},
		},
		{
			name: "leaked draft field",
			old:  "  string stable = 1;\n",
			new:  "  string stable = 1;\n  string a = 2;\n",
			want: []string{"stable descriptors differ", `"a"`},
		},
		{
			name: "lost stable comment",
			old:  "// Stable doc.\n",
			want: []string{`-1 "Stable doc."`},
		},
		{
			name: "lost trailing comment",
			old:  " // trailing stable_child\n",
			new:  "\n",
			want: []string{`-1 "trailing stable_child"`},
		},
		{
			name: "leaked draft comment",
			old:  "message StableChild {",
			new:  "// DRAFT leading a\nmessage StableChild {",
			want: []string{`+1 "DRAFT leading a"`},
		},
		{
			name: "import not in the input",
			old:  "package fixture;\n",
			new:  "package fixture;\n\nimport \"google/protobuf/descriptor.proto\";\n",
			want: []string{"import google/protobuf/descriptor.proto is not an import of the input"},
		},
		{
			name: "missing header",
			old:  testConfig.Header,
			want: []string{"missing generated header"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text := string(correct)
			if tc.old != "" {
				if strings.Count(text, tc.old) != 1 {
					t.Fatalf("output must contain %q once", tc.old)
				}
				text = strings.Replace(text, tc.old, tc.new, 1)
			}
			out := t.TempDir()
			if err := os.MkdirAll(filepath.Join(out, filepath.Dir(path)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, path), []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			problems, err := Check(testConfig, "testdata/input", out)
			if err != nil {
				t.Fatal(err)
			}
			if len(tc.want) == 0 {
				if len(problems) > 0 {
					t.Fatalf("want no problems, got:\n%s", strings.Join(problems, "\n"))
				}
				return
			}
			for _, problem := range problems {
				if containsAll(problem, tc.want) {
					return
				}
			}
			t.Fatalf("want a problem containing %q, got:\n%s", tc.want, strings.Join(problems, "\n"))
		})
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
