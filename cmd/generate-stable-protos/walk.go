package main

import (
	"github.com/bufbuild/protocompile/experimental/ast"
	"github.com/bufbuild/protocompile/experimental/ir"
	"github.com/bufbuild/protocompile/experimental/seq"
)

// findDraftNodes walks a single lowered file's top-level declarations
// (types, extensions, services) and returns the AST nodes of every
// draft-tagged declaration, ready to hand to edit.ApplyEdits
func findDraftNodes(tags draftTags, f *ir.File) []ast.DeclAny {
	var dels []ast.DeclAny

	for t := range seq.Values(f.Types()) {
		walkType(tags, t, &dels)
	}
	// TBD: are "experimental" extensions really needed?
	for ext := range seq.Values(f.Extensions()) {
		if isDraftMember(tags, ext) {
			dels = append(dels, ext.AST().AsAny())
		}
	}
	for s := range seq.Values(f.Services()) {
		if isDraftService(tags, s) {
			dels = append(dels, s.AST().AsAny())
			continue
		}
		for m := range seq.Values(s.Methods()) {
			if isDraftMethod(tags, m) {
				dels = append(dels, m.AST().AsAny())
			}
		}
	}

	return dels
}

func walkType(tags draftTags, t ir.Type, dels *[]ast.DeclAny) {
	if isDraftType(tags, t) {
		*dels = append(*dels, t.AST().AsAny())
		return
	}

	// TBD: oneOfs should be simpler/guarded by compiler
	draftByOneof := map[ir.Oneof][]ir.Member{}
	for m := range seq.Values(t.Members()) {
		if m.IsSynthetic() || !isDraftMember(tags, m) {
			continue
		}
		if o := m.Oneof(); !o.IsZero() {
			draftByOneof[o] = append(draftByOneof[o], m)
			continue
		}
		*dels = append(*dels, m.AST().AsAny())
	}

	for o := range seq.Values(t.Oneofs()) {
		draft := draftByOneof[o]
		total := o.Members().Len()
		if total > 0 && len(draft) == total {
			// Cascade: every branch is draft, so the oneof itself goes.
			*dels = append(*dels, o.AST().AsAny())
			continue
		}
		for _, m := range draft {
			*dels = append(*dels, m.AST().AsAny())
		}
	}

	for ext := range seq.Values(t.Extensions()) {
		if isDraftMember(tags, ext) {
			*dels = append(*dels, ext.AST().AsAny())
		}
	}
	for nested := range seq.Values(t.Nested()) {
		if nested.IsMapEntry() {
			// Synthetic type; nested.AST() aliases the map field's own
			// DeclDef, which is already handled as a regular field above
			// (or via the map field's own draft tag, if any).
			continue
		}
		walkType(tags, nested, dels)
	}
}
