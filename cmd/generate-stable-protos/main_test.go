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
