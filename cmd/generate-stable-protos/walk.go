package main

import (
	"github.com/bufbuild/protocompile/experimental/ast"
	"github.com/bufbuild/protocompile/experimental/ir"
	"github.com/bufbuild/protocompile/experimental/seq"
)

func findDraftNodes(tags draftTags, f *ir.File) []ast.DeclAny {
	var dels []ast.DeclAny
	for t := range seq.Values(f.Types()) {
		walkType(tags, t, &dels)
	}
	return dels
}

func walkType(tags draftTags, t ir.Type, dels *[]ast.DeclAny) {
	if isDraftType(tags, t) {
		*dels = append(*dels, t.AST().AsAny())
		return
	}
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
		if total := o.Members().Len(); total > 0 && len(draft) == total {
			// An empty oneof is invalid, so remove the declaration with its branches.
			*dels = append(*dels, o.AST().AsAny())
			continue
		}
		for _, m := range draft {
			*dels = append(*dels, m.AST().AsAny())
		}
	}
	for nested := range seq.Values(t.Nested()) {
		// Synthetic map entries share the field's AST, already handled above.
		if !nested.IsMapEntry() {
			walkType(tags, nested, dels)
		}
	}
}
