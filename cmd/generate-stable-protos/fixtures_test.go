package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
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
		before := readTree(t, out)
		err := testConfig.stableProtosGenerator(t.Context(), root, out)
		if err == nil || !strings.Contains(err.Error(), expected.Error) {
			t.Fatalf("want error %q, got %v", expected.Error, err)
		}
		if !maps.Equal(before, readTree(t, out)) {
			t.Fatal("generation failure modified output")
		}
		return
	}

	if err := testConfig.stableProtosGenerator(t.Context(), root, out); err != nil {
		t.Fatal(err)
	}
	actual := readTree(t, out)
	again := t.TempDir()
	if err := testConfig.stableProtosGenerator(t.Context(), root, again); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(actual, readTree(t, again)) {
		t.Fatal("generator output is not deterministic")
	}
	checkIdempotent(t.Context(), t, root, out, t.TempDir())

	paths, err := testConfig.getAPINextPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, annotations, err := testConfig.loadAPINext(t.Context(), root, paths)
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
}

var testConfig = defaultConfig()

func sortedKeys(a, b map[string]string) []string {
	keys := maps.Clone(a)
	maps.Copy(keys, b)
	return slices.Sorted(maps.Keys(keys))
}

// checkIdempotent feeds the stable tree the generator wrote from root into first back in
// as an api_next tree without drafts. The generator must then reproduce it byte for
// byte: removing nothing, keeping every comment, and printing its own output unchanged.
// scratch is an empty directory for the second pass.
func checkIdempotent(ctx context.Context, t testingT, root, first, scratch string) {
	t.Helper()
	next := filepath.Join(scratch, "next")
	// An import may have comments before its path, and the first literal of a path split
	// across literals is "temporal/api" on its own.
	stableImport := regexp.MustCompile(`(?m)^(import\b(?:\s+|/\*[^*]*\*/|public|weak)*["'])` + regexp.QuoteMeta(testConfig.stableDir) + `(["'/])`)
	for path, text := range readTree(t, first) {
		body, ok := strings.CutPrefix(text, generatedHeader)
		if !ok {
			t.Fatalf("%s: missing generated header", path)
		}
		body = stableImport.ReplaceAllString(body, `${1}`+testConfig.nextDir+`$2`)
		writeFile(t, filepath.Join(next, testConfig.nextDir, strings.TrimPrefix(path, testConfig.stableDir+"/")), body)
	}
	draft, err := os.ReadFile(filepath.Join(root, testConfig.draftSchema))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(next, testConfig.draftSchema), string(draft))
	// Imports outside temporal/, such as google/ and nexusannotations/, resolve as before.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "temporal" {
			continue
		}
		target, err := filepath.Abs(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(next, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}

	second := filepath.Join(scratch, "second")
	if err := testConfig.stableProtosGenerator(ctx, next, second); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	want, got := readTree(t, first), readTree(t, second)
	for _, path := range sortedKeys(want, got) {
		if want[path] != got[path] {
			t.Errorf("%s changed on the second pass\nfirst:\n%s\nsecond:\n%s", path, want[path], got[path])
		}
	}
}

// testingT is the part of testing.TB the fixture helpers use, which *rapid.T also has.
type testingT interface {
	Helper()
	Errorf(format string, args ...any)
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

func writeFile(t testingT, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
