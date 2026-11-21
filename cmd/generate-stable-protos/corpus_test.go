package main

import "testing"

// TestRepoAPINextIdempotent checks that the stable tree generated from the repository's
// api_next tree is a fixed point of the generator.
func TestRepoAPINextIdempotent(t *testing.T) {
	t.Parallel()
	const root = "../.."
	first := t.TempDir()
	if err := stableProtosGenerator(t.Context(), root, first); err != nil {
		t.Fatal(err)
	}
	checkIdempotent(t, root, first)
}
