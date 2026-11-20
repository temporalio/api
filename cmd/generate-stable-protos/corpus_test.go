package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/temporalio/api/cmd/generate-stable-protos/internal/oracle"
)

// TestRepoAPINext generates the stable tree from the repository's own api_next tree and
// checks it against the oracle, so real-world declarations, options and comments are
// covered beyond the hand-written fixtures.
func TestRepoAPINext(t *testing.T) {
	t.Parallel()
	const root = "../.."
	out := t.TempDir()
	if err := stableProtosGenerator(t.Context(), root, out); err != nil {
		t.Fatal(err)
	}
	problems, err := oracle.Check(oracleConfig, root, out)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

// TestRepoAPINextIdempotent feeds the stable tree generated from the repository's
// api_next tree back in as an api_next tree without drafts. The generator must then
// reproduce it byte for byte: removing nothing, keeping every comment, and printing
// its own output unchanged.
func TestRepoAPINextIdempotent(t *testing.T) {
	t.Parallel()
	const root = "../.."
	first := t.TempDir()
	if err := stableProtosGenerator(t.Context(), root, first); err != nil {
		t.Fatal(err)
	}

	next := t.TempDir()
	stableImport := regexp.MustCompile(`(?m)^(import\s+(?:public\s+|weak\s+)?)"` + regexp.QuoteMeta(stableAPIPrefix))
	for path, text := range readTree(t, first) {
		body, ok := strings.CutPrefix(text, generatedHeader)
		if !ok {
			t.Fatalf("%s: missing generated header", path)
		}
		body = stableImport.ReplaceAllString(body, `${1}"`+apiNextPrefix)
		writeFile(t, filepath.Join(next, apiNextPrefix, strings.TrimPrefix(path, stableAPIPrefix)), body)
	}
	draft, err := os.ReadFile(filepath.Join(root, draftTagsPath))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(next, draftTagsPath), string(draft))
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

	second := t.TempDir()
	if err := stableProtosGenerator(t.Context(), next, second); err != nil {
		t.Fatal(err)
	}
	want, got := readTree(t, first), readTree(t, second)
	for _, path := range sortedKeys(want, got) {
		if want[path] != got[path] {
			t.Errorf("%s changed on the second pass\nfirst:\n%s\nsecond:\n%s", path, want[path], got[path])
		}
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
