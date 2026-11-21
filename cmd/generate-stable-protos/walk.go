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
	for service := range seq.Values(f.Services()) {
		if tags.has(service.Options()) {
			dels = append(dels, service.AST().AsAny())
			continue
		}
		for method := range seq.Values(service.Methods()) {
			if tags.has(method.Options()) {
				dels = append(dels, method.AST().AsAny())
			}
		}
	}
	return dels
}

func walkType(tags draftTags, t ir.Type, dels *[]ast.DeclAny) {
	if tags.has(t.Options()) {
		*dels = append(*dels, t.AST().AsAny())
		return
	}
	for o := range seq.Values(t.Oneofs()) {
		if tags.has(o.Options()) {
			// An empty oneof is invalid, so remove the declaration with its branches.
			*dels = append(*dels, o.AST().AsAny())
		}
	}
	for m := range seq.Values(t.Members()) {
		if m.IsSynthetic() || !tags.has(m.Options()) {
			continue
		}
		*dels = append(*dels, m.AST().AsAny())
	}
	for nested := range seq.Values(t.Nested()) {
		// Synthetic map entries share the AST of the field handled above.
		if !nested.IsMapEntry() {
			walkType(tags, nested, dels)
		}
	}
}
