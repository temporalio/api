package main

import (
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
	if err := testConfig.stableProtosGenerator(t.Context(), root, out); err != nil {
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

// TestRepoAPINextIdempotent checks that the stable tree generated from the repository's
// api_next tree is a fixed point of the generator.
func TestRepoAPINextIdempotent(t *testing.T) {
	t.Parallel()
	const root = "../.."
	first := t.TempDir()
	if err := testConfig.stableProtosGenerator(t.Context(), root, first); err != nil {
		t.Fatal(err)
	}
	checkIdempotent(t.Context(), t, root, first, t.TempDir())
}
