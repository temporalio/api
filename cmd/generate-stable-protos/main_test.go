package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

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
