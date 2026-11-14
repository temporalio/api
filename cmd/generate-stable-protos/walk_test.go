package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftSchemaValidation(t *testing.T) {
	schema, err := os.ReadFile("testdata/classification/valid/input/temporal/api_next/protometa/v1/draft.proto")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, schema, want string }{
		{"wrong type", strings.Replace(string(schema), "string draft =", "bool draft =", 1), "invalid draft option schema"},
		{"wrong extendee", strings.Replace(string(schema), "FieldOptions", "OneofOptions", 1), "invalid draft option schema"},
		{"missing extension", `syntax = "proto3"; package temporal.api.protometa.v1;`, "not found"},
		// An extension outside the draft name table is not a draft option and needs no schema check.
		{"unrelated extension", string(schema) + "extend google.protobuf.FileOptions { bool draft_notes = 98112; }\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, draftTagsPath)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.schema), 0600); err != nil {
				t.Fatal(err)
			}
			err := stableProtosGenerator(root, t.TempDir())
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
