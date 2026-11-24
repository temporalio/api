package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftCollections(t *testing.T) {
	schema, err := os.ReadFile("../../temporal/api_next/protometa/v1/draft.proto")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		proto string
		want  string
	}{
		{
			name: "stable repeated message reaches draft through a cycle",
			proto: `
message Root { repeated Item entries = 1; }
message Item { Nested nested = 1; }
message Nested {
  Item cycle = 1;
  string secret = 2 [(temporal.api.protometa.v1.draft) = "preview"];
}`,
			want: "fixture.Root.entries: retained collection reaches draft declaration fixture.Nested.secret",
		},
		{
			name: "stable map reaches an individually gated enum value",
			proto: `
message Root { map<string, Mode> entries = 1; }
enum Mode {
  MODE_UNSPECIFIED = 0;
  MODE_PREVIEW = 1 [(temporal.api.protometa.v1.draft_enum_value) = "preview"];
}`,
			want: "fixture.Root.entries: retained collection reaches draft declaration fixture.MODE_PREVIEW",
		},
		{
			name: "gated list reaches a different gate",
			proto: `
message Root { repeated Item entries = 1 [(temporal.api.protometa.v1.draft) = "preview"]; }
message Item {
  string secret = 1 [(temporal.api.protometa.v1.draft) = "details"];
}`,
			want: "fixture.Root.entries: retained collection reaches draft declaration fixture.Item.secret",
		},
		{
			name: "list and map clear under one gate",
			proto: `
message Root {
  repeated Item entries = 1 [(temporal.api.protometa.v1.draft) = "preview"];
  map<string, Item> by_name = 2 [(temporal.api.protometa.v1.draft) = "preview"];
}
message Item {
  option (temporal.api.protometa.v1.draft_message) = "preview";
  repeated string names = 1;
  Item cycle = 2;
}`,
		},
		{
			name: "gated collection can share a stable message with the same gate",
			proto: `
message Root { repeated Item entries = 1 [(temporal.api.protometa.v1.draft) = "preview"]; }
message Item {
  string secret = 1 [(temporal.api.protometa.v1.draft) = "preview"];
}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, cfg.draftSchema), string(schema))
			writeFile(t, filepath.Join(root, cfg.nextDir, "fixture.proto"), `syntax = "proto3";
package fixture;
import "temporal/api_next/protometa/v1/draft.proto";
`+tc.proto)
			err := cfg.stableProtosGenerator(t.Context(), root, t.TempDir())
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}
