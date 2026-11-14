package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDraftCommentOwnership checks that no comment belonging to a removed draft
// declaration survives in the stable output: neither one protoc attributes to it, nor one
// after it that protoc attaches to nothing. The passing case also pins the raw printer
// output, which TestStableProjection cannot see because it runs buf format first.
func TestDraftCommentOwnership(t *testing.T) {
	const fixture = "temporal/api_next/fixture.proto"
	const after = `after a removed draft declaration would survive in the stable output; move it directly above a declaration, or delete it`
	input, err := os.ReadFile(filepath.Join("testdata/comments/input", fixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, new, want string }{
		{"attached comments are removed", "", "", ""},
		{
			"next-line trailing comment",
			"= \"f\"];\n",
			"= \"f\"];\n  // Next-line trailing comment of f.\n",
			`temporal/api_next/fixture.proto:16: comment "// Next-line trailing comment of f." ` + after,
		},
		{
			"detached comment before the next stable field",
			"= \"f\"];\n",
			"= \"f\"];\n\n  // Detached comment before d.\n",
			`temporal/api_next/fixture.proto:17: comment "// Detached comment before d." ` + after,
		},
		{
			"comment after a nested draft message",
			"  // leading h shares the word DRAFT\n",
			"  message Inner {\n    option (temporal.api.protometa.v1.draft_message) = \"inner\";\n  }\n  // Note after Inner.\n\n  // leading h shares the word DRAFT\n",
			`temporal/api_next/fixture.proto:30: comment "// Note after Inner." ` + after,
		},
		{
			"dangling comment after the last draft field",
			"  string h = 9;\n}",
			"  string h = 9;\n  string z = 10 [(temporal.api.protometa.v1.draft) = \"z\"];\n\n  // Note about z.\n}",
			`temporal/api_next/fixture.proto:31: comment "// Note about z." ` + after,
		},
		{
			"comment at end of file after a draft message",
			"message Flat { string x = 1; }\n",
			"message Flat { string x = 1; }\n\nmessage Tail {\n  option (temporal.api.protometa.v1.draft_message) = \"tail\";\n}\n// Note after Tail.\n",
			`temporal/api_next/fixture.proto:43: comment "// Note after Tail." ` + after,
		},
		{
			"detached section header",
			"}\n\n// DRAFT leading comment of draft message.",
			"}\n\n// Section header: draft messages follow.\n\n// DRAFT leading comment of draft message.",
			`comment "Section header: draft messages follow." of a removed draft declaration survives`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, out := t.TempDir(), t.TempDir()
			if err := os.CopyFS(root, os.DirFS("testdata/comments/input")); err != nil {
				t.Fatal(err)
			}
			if tc.old != "" && strings.Count(string(input), tc.old) != 1 {
				t.Fatalf("fixture must contain %q once", tc.old)
			}
			if err := os.WriteFile(filepath.Join(root, fixture), []byte(strings.Replace(string(input), tc.old, tc.new, 1)), 0600); err != nil {
				t.Fatal(err)
			}
			err := stableProtosGenerator(root, out)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want %q, got %v", tc.want, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile("testdata/comments/expected/temporal/api/fixture.proto")
			if err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(filepath.Join(out, "temporal/api/fixture.proto"))
			if err != nil {
				t.Fatal(err)
			}
			if body, ok := strings.CutPrefix(string(actual), generatedHeader); !ok || body != string(expected) {
				t.Fatalf("raw printer output differs\nexpected:\n%s\nactual:\n%s", expected, actual)
			}
		})
	}
}
