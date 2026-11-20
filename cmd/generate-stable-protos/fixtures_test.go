package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/temporalio/api/cmd/generate-stable-protos/internal/oracle"
)

// TestFixtures runs the generator on every fixture under testdata. A fixture is a
// directory with an input/ api_next tree, and optionally an expected.json and an
// expected/ tree holding the raw generator output without its generated header.
func TestFixtures(t *testing.T) {
	t.Parallel()
	for _, fixture := range findFixtures(t) {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			runFixture(t, filepath.Join("testdata", fixture))
		})
	}
}

// fixtureExpectation is a fixture's expected.json.
type fixtureExpectation struct {
	// Error is a substring of the expected generator error; empty means success.
	Error string `json:"error"`
	// Annotations are the draft annotations the generator must accept.
	Annotations draftAnnotations `json:"annotations"`
}

// findFixtures returns the fixture directories under testdata, relative to it.
func findFixtures(t *testing.T) []string {
	t.Helper()
	var fixtures []string
	if err := filepath.WalkDir("testdata", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return err
		}
		for _, marker := range []string{"input", "expected.json"} {
			if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
				relative, err := filepath.Rel("testdata", path)
				if err != nil {
					return err
				}
				fixtures = append(fixtures, filepath.ToSlash(relative))
				return filepath.SkipDir
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return fixtures
}

func runFixture(t *testing.T, dir string) {
	t.Helper()
	var expected fixtureExpectation
	if data, err := os.ReadFile(filepath.Join(dir, "expected.json")); err == nil {
		if err := json.Unmarshal(data, &expected); err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "input")
	out := t.TempDir()

	if expected.Error != "" {
		sentinel := map[string]string{"retained": "old"}
		if err := os.WriteFile(filepath.Join(out, "retained"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := stableProtosGenerator(t.Context(), root, out)
		if err == nil || !strings.Contains(err.Error(), expected.Error) {
			t.Fatalf("want error %q, got %v", expected.Error, err)
		}
		if !reflect.DeepEqual(sentinel, readTree(t, out)) {
			t.Fatal("generation failure modified output")
		}
		return
	}

	if err := stableProtosGenerator(t.Context(), root, out); err != nil {
		t.Fatal(err)
	}
	actual := readTree(t, out)
	again := t.TempDir()
	if err := stableProtosGenerator(t.Context(), root, again); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, readTree(t, again)) {
		t.Fatal("generator output is not deterministic")
	}

	paths, err := getAPINextPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, annotations, err := loadAPINext(t.Context(), root, paths)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(expected.Annotations, annotations) {
		t.Errorf("draft annotations differ\nexpected: %v\nactual: %v", expected.Annotations, annotations)
	}

	if _, err := os.Stat(filepath.Join(dir, "expected")); err == nil {
		bodies := map[string]string{}
		for path, text := range actual {
			body, ok := strings.CutPrefix(text, generatedHeader)
			if !ok {
				t.Fatalf("%s: missing generated header", path)
			}
			bodies[path] = body
		}
		golden := readTree(t, filepath.Join(dir, "expected"))
		for _, path := range sortedKeys(golden, bodies) {
			if golden[path] != bodies[path] {
				t.Errorf("%s differs from the golden\nexpected:\n%s\nactual:\n%s", path, golden[path], bodies[path])
			}
		}
	}

	problems, err := oracle.Check(oracleConfig, root, out)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

var oracleConfig = oracle.Config{
	NextPrefix:   apiNextPrefix,
	StablePrefix: stableAPIPrefix,
	DraftFile:    draftTagsPath,
	Header:       generatedHeader,
}

func sortedKeys(a, b map[string]string) []string {
	keys := maps.Clone(a)
	maps.Copy(keys, b)
	return slices.Sorted(maps.Keys(keys))
}
