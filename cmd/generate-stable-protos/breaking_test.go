package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftBreakingReservations(t *testing.T) {
	config, err := filepath.Abs("../../buf.next.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const schema = `syntax = "proto3";
package fixture;
import "google/protobuf/descriptor.proto";
extend google.protobuf.FieldOptions { string draft = 98008; }
message Example { %s }
`
	image := func(t *testing.T, field string) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "temporal/api_next/fixture.proto")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(schema, "%s", field, 1)), 0600); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(dir, "schema.binpb")
		if out, err := exec.Command("protoc", "-I"+dir, "--include_imports", "--include_source_info", "--descriptor_set_out="+output, path).CombinedOutput(); err != nil {
			t.Fatalf("compile fixture: %v\n%s", err, out)
		}
		return output
	}
	before := image(t, `string value = 1 [(draft) = "feature-a"];`)
	for _, tc := range []struct {
		name, field string
		want        []string
	}{
		{"deleted", "", []string{"without reserving the number", "without reserving the name"}},
		{"number only", "reserved 1;", []string{"without reserving the name"}},
		{"name only", `reserved "value";`, []string{"without reserving the number"}},
		{"both", `reserved 1; reserved "value";`, nil},
		{"promoted", "string value = 1;", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := image(t, tc.field)
			out, err := exec.Command("buf", "breaking", after, "--against", before, "--config", config).CombinedOutput()
			if len(tc.want) == 0 {
				if err != nil {
					t.Fatalf("compatible change: %v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatal("unreserved field deletion passed breaking check")
			}
			for _, want := range tc.want {
				if !strings.Contains(string(out), want) {
					t.Fatalf("want %q: %s", want, out)
				}
			}
		})
	}
}

func TestDraftBreakingBootstrap(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path, schema string
		bootstrap          bool
	}{
		{"no draft history", "temporal/api/example.proto", `syntax = "proto3"; message Stable {}`, true},
		{"invalid draft history", "temporal/api_next/example.proto", "invalid proto", false},
		// Only Buf's whole "had no .proto files" failure line means bootstrap, not any mention of it.
		{"invalid draft history naming the bootstrap phrase", "temporal/api_next/no .proto files/example.proto", "invalid proto", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := t.TempDir()
			path := filepath.Join(baseline, tc.path)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.schema), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "scripts/check-api-next-breaking.sh", baseline)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if tc.bootstrap {
				if err != nil || !strings.Contains(string(out), "api_next has no baseline schema yet") {
					t.Fatalf("expected explicit bootstrap: %v\n%s", err, out)
				}
			} else if err == nil || strings.Contains(string(out), "no baseline schema") {
				t.Fatalf("invalid baseline must fail: %v\n%s", err, out)
			}
		})
	}
}
