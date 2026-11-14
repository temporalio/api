package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStableProjection(t *testing.T) {
	for _, fixture := range []string{"classification/valid", "classification/message-types", "imports"} {
		t.Run(fixture, func(t *testing.T) {
			expected := readTree(t, filepath.Join("testdata", fixture, "expected"))
			for range 2 {
				out := t.TempDir()
				if err := stableProtosGenerator(filepath.Join("testdata", fixture, "input"), out); err != nil {
					t.Fatal(err)
				}
				formatTree(t, out)
				actual := readTree(t, out)
				if !reflect.DeepEqual(expected, actual) {
					t.Fatalf("projection differs from independently authored fixture\nexpected: %v\nactual: %v", expected, actual)
				}
			}
		})
	}
}

func TestAnnotationClassification(t *testing.T) {
	dirs, err := os.ReadDir("testdata/classification")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		t.Run(dir.Name(), func(t *testing.T) {
			fixture := filepath.Join("testdata/classification", dir.Name())
			data, err := os.ReadFile(filepath.Join(fixture, "expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			var expected struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(data, &expected); err != nil {
				t.Fatal(err)
			}
			err = stableProtosGenerator(filepath.Join(fixture, "input"), t.TempDir())
			if expected.Error == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), expected.Error) {
				t.Fatalf("want %q, got %v", expected.Error, err)
			}
		})
	}
}

func TestProjectionFailures(t *testing.T) {
	for _, fixture := range []string{"dangling", "missing"} {
		t.Run(fixture, func(t *testing.T) {
			out := t.TempDir()
			sentinel := filepath.Join(out, "retained")
			if err := os.WriteFile(sentinel, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			before := readTree(t, out)
			if err := stableProtosGenerator(filepath.Join("testdata", fixture, "input"), out); err == nil {
				t.Fatal("expected invalid input to fail")
			}
			if !reflect.DeepEqual(before, readTree(t, out)) {
				t.Fatal("generation failure modified output")
			}
		})
	}
}

func TestRewriteImportPaths(t *testing.T) {
	input := `syntax = "proto3";
import public /* before */ 'temporal/api_next/used.proto' /* after */;
import "google/protobuf/any.proto";
import "temporal/api_n" /* internal comment */ "ext/another.proto";
option java_package = "temporal/api_next/used.proto";
// import "temporal/api_next/used.proto";
`
	expected := `syntax = "proto3";
import public /* before */ "temporal/api/used.proto" /* after */;
import "google/protobuf/any.proto";
import "temporal/api" /* internal comment */ "/another.proto";
option java_package = "temporal/api_next/used.proto";
// import "temporal/api_next/used.proto";
`
	actual, err := rewriteImportPaths(input)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("expected %q, got %q", expected, actual)
	}
	if _, err := rewriteImportPaths(`import "unterminated`); err == nil {
		t.Fatal("expected invalid syntax to fail")
	}
}

func TestStableReplacement(t *testing.T) {
	script, err := filepath.Abs("../../scripts/stable-api.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"generation", "formatting", "replacement", "none"} {
		t.Run(failure, func(t *testing.T) {
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
				if !reflect.DeepEqual(readTree(t, filepath.Join(root, "temporal/api")), map[string]string{"new.proto": "generated"}) {
					t.Fatal("new tree was nested or old files survived")
				}
			} else {
				if err == nil {
					t.Fatal("expected injected failure")
				}
				if !reflect.DeepEqual(before, readTree(t, filepath.Join(root, "temporal/api"))) {
					t.Fatalf("%s failure modified old tree: %s", failure, output)
				}
			}
		})
	}
}

func formatTree(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("buf", "format", "-w", root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("format: %v: %s", err, output)
	}
}

func readTree(t *testing.T, root string) map[string]string {
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
