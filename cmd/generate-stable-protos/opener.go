package main

import (
	"os"

	"github.com/bufbuild/protocompile/experimental/source"
)

// diskOpener resolves repo-relative proto paths ("temporal/api_next/x.proto")
// against a directory on disk.
func diskOpener(root string) source.Opener {
	return &source.FS{FS: os.DirFS(root)}
}

// baseOpener serves the disk corpus and well-known types. Every pass that
// reads real files unmodified uses it (currently just stripDraftChanges,
// which lowers the authored api_next tree as-is). Construct this once per
// pass and share it across every query in that pass. See the comparability
// note on memOpener below for why.
func baseOpener(root string) source.Opener {
	return &source.Openers{source.WKTs(), diskOpener(root)}
}

// memOpener serves an entire in-memory set of final texts (keyed by their
// paths in whatever path space the caller is currently working in) ahead
// of the disk corpus and WKTs. The stripUnusedImports pass uses it to
// re-lower draft-stripped text, and verifyStrippedProtos uses it for
// cross-file verification of the final stable tree.
//
// Implementations of source.Opener must be comparable because an Opener value
// is part of queries.IR's cache key (see queries.IR.Key() in protocompile).
// Every constructor in this file therefore returns a pointer, and callers
// must call the constructor exactly ONCE per lowering pass and share the
// resulting Opener across every query in that pass. Calling it again
// (even with identical contents) yields a distinct pointer, hence a
// distinct cache key, forcing protocompile to re-lower the whole corpus.
func memOpener(root string, files map[string]string) source.Opener {
	m := source.NewMap(nil)
	for path, text := range files {
		m.Add(path, text)
	}
	return &source.Openers{m, baseOpener(root)}
}
