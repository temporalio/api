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

// baseOpener is the disk corpus plus well-known types, shared by every pass
// that reads real files unmodified (currently just stripExperimentalChanges,
// which lowers the authored api_next tree as-is). Construct this once per
// pass and share it across every query in that pass; see the comparability
// note on memOpener below for why.
func baseOpener(root string) source.Opener {
	return &source.Openers{source.WKTs(), diskOpener(root)}
}

// memOpener serves an entire in-memory set of final texts (keyed by their
// paths in whatever path space the caller is currently working in) ahead
// of the disk corpus and WKTs. Used by stripUnusedImports (re-lowering
// draft-stripped text) and verifyStrippedProtos (combined cross-file
// verification of the final stable tree).
//
// source.Opener implementations must be comparable: an Opener value is
// part of queries.IR's cache key (see queries.IR.Key() in protocompile).
// Every constructor in this file therefore returns a pointer, and callers
// must call the constructor exactly ONCE per lowering pass and share the
// resulting Opener across every query in that pass -- calling it again
// (even with identical contents) yields a distinct pointer, hence a
// distinct cache key, forcing protocompile to re-lower the whole corpus.
func memOpener(root string, files map[string]string) source.Opener {
	m := source.NewMap(nil)
	for path, text := range files {
		m.Add(path, text)
	}
	return &source.Openers{m, baseOpener(root)}
}
