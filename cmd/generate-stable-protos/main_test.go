package main

import (
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bufbuild/protocompile/experimental/parser"
	"github.com/bufbuild/protocompile/experimental/report"
	"github.com/bufbuild/protocompile/experimental/source"
)

func TestGeneratorWithCustomPaths(t *testing.T) {
	t.Parallel()
	root, out := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(root, "next/meta/options.proto"), `syntax = "proto3";
package example.meta;
import "google/protobuf/descriptor.proto";
extend google.protobuf.FieldOptions { string draft = 50000; }
extend google.protobuf.MessageOptions { string draft_message = 50000; }
`)
	writeFile(t, filepath.Join(root, "next/shared.proto"), `syntax = "proto3";
package example.api;
message Shared {}
`)
	writeFile(t, filepath.Join(root, "next/service.proto"), `syntax = "proto3";
package example.api;
import "ne" /* Keep this comment. */ "xt/shared.proto";
import "next/meta/options.proto";
message Request {
  Shared shared = 1;
  string draft_field = 2 [(example.meta.draft) = "feature"];
}
message DraftMessage {
  option (example.meta.draft_message) = "feature";
}
`)

	cmd := exec.Command("go", "run", ".", "-root", root, "-out", out,
		"-next", "next", "-stable", "stable", "-draft-schema", "next/meta/options.proto")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate with custom paths: %v\n%s", err, output)
	}
	files := readTree(t, out)
	if paths := slices.Sorted(maps.Keys(files)); !slices.Equal(paths, []string{"stable/service.proto", "stable/shared.proto"}) {
		t.Fatalf("unexpected generated files: %v", paths)
	}
	service := files["stable/service.proto"]
	var diagnostics report.Report
	file, _ := parser.Parse("stable/service.proto", source.NewFile("stable/service.proto", service), &diagnostics)
	if errs, hasErrors := errorDiagnostics(&diagnostics); hasErrors {
		t.Fatalf("parse generated service: %v\n%s", errs, service)
	}
	var imports []string
	for imp := range file.Imports() {
		imports = append(imports, imp.ImportPath().AsLiteral().Token.AsString().Text())
	}
	if !slices.Equal(imports, []string{"stable/shared.proto"}) {
		t.Fatalf("unexpected generated imports: %v\n%s", imports, service)
	}
	for _, want := range []string{"Keep this comment.", "Shared shared = 1;"} {
		if !strings.Contains(service, want) {
			t.Errorf("missing %q in generated service:\n%s", want, service)
		}
	}
	for _, draft := range []string{"draft_field", "DraftMessage"} {
		if strings.Contains(service, draft) {
			t.Errorf("found %q in generated service:\n%s", draft, service)
		}
	}
}

func TestGeneratorRejectsInvalidPaths(t *testing.T) {
	t.Parallel()
	generator := filepath.Join(t.TempDir(), "generate-stable-protos")
	if output, err := exec.Command("go", "build", "-o", generator, ".").CombinedOutput(); err != nil {
		t.Fatalf("build generator: %v\n%s", err, output)
	}
	for _, tc := range []struct {
		name, next, stable, source, schema, wantError string
	}{
		{"root source", ".", "stable", "./service.proto", "meta/draft.proto", "-next"},
		{"root destination", "next", ".", "next/service.proto", "meta/draft.proto", "-stable"},
		{"source traversal", "../next", "stable", "../next/service.proto", "meta/draft.proto", "-next"},
		{"destination traversal", "next", "../outside", "next/service.proto", "meta/draft.proto", "-stable"},
		{"absolute source", "$ROOT/next", "stable", "next/service.proto", "meta/draft.proto", "-next"},
		{"absolute destination", "next", "$ROOT/stable", "next/service.proto", "meta/draft.proto", "-stable"},
		{"unclean source", "./next", "stable", "next/service.proto", "meta/draft.proto", "-next"},
		{"unclean destination", "next", "stable/../output", "next/service.proto", "meta/draft.proto", "-stable"},
		{"absolute schema", "next", "stable", "next/service.proto", "$ROOT/meta/draft.proto", "-draft-schema"},
		{"schema traversal", "next", "stable", "next/service.proto", "../draft.proto", "-draft-schema"},
		{"unclean schema", "next", "stable", "next/service.proto", "./meta/draft.proto", "-draft-schema"},
		{"equal directories", "next", "next", "next/service.proto", "meta/draft.proto", "overlap"},
		{"destination within source", "next", "next/stable", "next/service.proto", "meta/draft.proto", "overlap"},
		{"source within destination", "stable/next", "stable", "stable/next/service.proto", "meta/draft.proto", "overlap"},
		{"source file", "source.proto", "stable", "source.proto", "meta/draft.proto", "-next \"source.proto\" must be a directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workspace := t.TempDir()
			root := filepath.Join(workspace, "root")
			writeFile(t, filepath.Join(root, tc.source), `syntax = "proto3";
package example.api;
message Request {}
`)
			writeFile(t, filepath.Join(root, "meta/draft.proto"), `syntax = "proto3";
package example.meta;
import "google/protobuf/descriptor.proto";
extend google.protobuf.FieldOptions { string draft = 50000; }
`)
			before := readTree(t, workspace)
			next := strings.ReplaceAll(tc.next, "$ROOT", filepath.ToSlash(root))
			stable := strings.ReplaceAll(tc.stable, "$ROOT", filepath.ToSlash(root))
			schema := strings.ReplaceAll(tc.schema, "$ROOT", filepath.ToSlash(root))
			cmd := exec.Command(generator, "-root", root, "-out", filepath.Join(workspace, "out"),
				"-next", next, "-stable", stable, "-draft-schema", schema)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.wantError) {
				t.Errorf("expected rejection mentioning %q, got %v\n%s", tc.wantError, err, output)
			}
			after := readTree(t, workspace)
			if !maps.Equal(before, after) {
				t.Errorf("tree changed after rejected generation\nbefore: %v\nafter: %v", before, after)
			}
		})
	}
}

func TestStableAPIBufGuards(t *testing.T) {
	t.Parallel()
	toolDir := t.TempDir()
	for name, script := range map[string]string{
		"go": "exit 0",
		"buf": `if [[ "$*" == "ls-files --config buf.next.yaml" ]]; then
  printf '%s\n' "$NEXT_FILES"
  exit "$NEXT_STATUS"
fi
printf '%s\n' "$DEFAULT_FILES"
exit "$DEFAULT_STATUS"`,
		"grep": `if [[ "$FILTER_FAIL" == 1 ]]; then exit 2; fi
exec /usr/bin/grep "$@"`,
		"awk": `if [[ "$FILTER_FAIL" == 1 ]]; then exit 2; fi
exec /usr/bin/awk "$@"`,
	} {
		if err := os.WriteFile(filepath.Join(toolDir, name), []byte("#!/bin/bash\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, defaultFiles, nextFiles, defaultStatus, nextStatus, filterFail string
		wantFailure                                                          bool
	}{
		{"clean listings", "temporal/api/service.proto", "temporal/api_next/service.proto", "0", "0", "0", false},
		{"preview file in default listing", "temporal/api_next/service.proto", "", "0", "0", "0", true},
		{"stable file in preview listing", "", "temporal/api/service.proto", "0", "0", "0", true},
		{"fixture in default listing", "cmd/fixture.proto", "", "0", "0", "0", true},
		{"fixture in preview listing", "", "cmd/fixture.proto", "0", "0", "0", true},
		{"default Buf failure", "", "", "2", "0", "0", true},
		{"preview Buf failure", "", "", "0", "2", "0", true},
		{"filter failure", "", "", "0", "0", "1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("make", "-C", "../..", "stable-api-test", "PATH="+toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			cmd.Env = append(os.Environ(), "DEFAULT_FILES="+tc.defaultFiles, "NEXT_FILES="+tc.nextFiles,
				"DEFAULT_STATUS="+tc.defaultStatus, "NEXT_STATUS="+tc.nextStatus, "FILTER_FAIL="+tc.filterFail)
			output, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantFailure {
				t.Fatalf("want failure %t, got %v\n%s", tc.wantFailure, err, output)
			}
		})
	}
}

func TestGeneratorValidatesRequestHeaderWithRelocatedDraftSchema(t *testing.T) {
	t.Parallel()
	root, out := t.TempDir(), t.TempDir()
	const schema = "temporal/api_next/custom/options.proto"
	for path, body := range readTree(t, "testdata/classification/rpc-request-header/input") {
		if path == oracleConfig.DraftFile {
			path = schema
			body = strings.Replace(body, "package temporal.api.protometa.v1;", "package example.meta;", 1)
		} else {
			body = strings.NewReplacer(
				oracleConfig.DraftFile, schema,
				"temporal.api.protometa.v1.draft", "example.meta.draft",
			).Replace(body)
		}
		writeFile(t, filepath.Join(root, path), body)
	}

	cmd := exec.Command("go", "run", ".", "-root", root, "-out", out, "-draft-schema", schema)
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "fixture.Service.Call: request header references draft field item.draft_id") {
		t.Fatalf("expected rejection of the draft header field reference, got %v\n%s", err, output)
	}
}

func TestStableReplacement(t *testing.T) {
	t.Parallel()
	script, err := filepath.Abs("../../scripts/stable-api.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"generation", "formatting", "replacement", "none"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			tools := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "temporal/api/nested"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "temporal/api/nested/old.proto"), []byte("retained"), 0600); err != nil {
				t.Fatal(err)
			}
			before := readTree(t, filepath.Join(root, "temporal/api"))
			writeTool := func(name, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(tools, name), []byte("#!/bin/bash\nset -eu\n"+body), 0755); err != nil {
					t.Fatal(err)
				}
			}
			writeTool("go", `[[ "$FAILURE" != generation ]] || exit 1
while [[ "$1" != -out ]]; do shift; done
mkdir -p "$2/temporal/api"
printf generated > "$2/temporal/api/new.proto"
`)
			writeTool("buf", `[[ "$FAILURE" != formatting ]]
`)
			writeTool("mv", `if [[ "$FAILURE" == replacement && "$1" == */temporal/api && "$1" == */.stable-api.* ]]; then exit 1; fi
exec /bin/mv "$@"
`)
			cmd := exec.Command("bash", script, root)
			cmd.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "GO="+filepath.Join(tools, "go"), "BUF="+filepath.Join(tools, "buf"), "FAILURE="+failure)
			output, err := cmd.CombinedOutput()
			if failure == "none" {
				if err != nil {
					t.Fatalf("%v: %s", err, output)
				}
				if !maps.Equal(readTree(t, filepath.Join(root, "temporal/api")), map[string]string{"new.proto": "generated"}) {
					t.Fatal("new tree was nested or old files survived")
				}
			} else {
				if err == nil {
					t.Fatal("expected injected failure")
				}
				if !maps.Equal(before, readTree(t, filepath.Join(root, "temporal/api"))) {
					t.Fatalf("%s failure modified old tree: %s", failure, output)
				}
			}
		})
	}
}

func readTree(t testingT, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}
