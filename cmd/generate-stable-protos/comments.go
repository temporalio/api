package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bufbuild/protocompile/experimental/ast"
	"github.com/bufbuild/protocompile/experimental/fdp"
	"github.com/bufbuild/protocompile/experimental/ir"
	"github.com/bufbuild/protocompile/experimental/token"
)

// draftComments holds the comments protoc attributes to a file's removed draft
// declarations, and how often each comment word may still occur in the stable file.
type draftComments struct {
	comments []struct{ at, text string }
	allowed  map[string]int
}

func findDraftComments(f *ir.File, dels []ast.DeclAny) (draftComments, error) {
	if err := checkCommentsAfter(f, dels); err != nil {
		return draftComments{}, err
	}
	fd, err := fdp.DescriptorProto(f, fdp.IncludeSourceCodeInfo(true))
	if err != nil {
		return draftComments{}, fmt.Errorf("source info for %s: %w", f.Path(), err)
	}
	found := draftComments{allowed: commentWords(f.AST().Stream())}
	for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
		// Locations nest, so one starting inside a removed declaration belongs to it.
		start := loc.GetSpan()[:2]
		if !slices.ContainsFunc(dels, func(d ast.DeclAny) bool {
			ds, de := d.Span().StartLoc(), d.Span().EndLoc()
			return slices.Compare([]int32{int32(ds.Line - 1), int32(ds.Column - 1)}, start) <= 0 &&
				slices.Compare(start, []int32{int32(de.Line - 1), int32(de.Column - 1)}) < 0
		}) {
			continue
		}
		// Each comment paragraph belongs to one location, so none is counted twice.
		for _, text := range append(slices.Clone(loc.GetLeadingDetachedComments()), loc.GetLeadingComments(), loc.GetTrailingComments()) {
			if text = strings.TrimSpace(text); text != "" {
				found.comments = append(found.comments, struct{ at, text string }{fmt.Sprintf("%s:%d", f.Path(), start[0]+1), text})
				for _, w := range strings.Fields(text) {
					found.allowed[w]--
				}
			}
		}
	}
	return found, nil
}

// checkCommentsAfter rejects comments between a removed declaration and the next code.
// protoc attaches such comments to the removed declaration or to nothing, and the printer
// then keeps them. Only a comment on the declaration's own last line, or the doc block
// directly above the next surviving declaration, may sit there.
func checkCommentsAfter(f *ir.File, dels []ast.DeclAny) error {
	removed := func(offset int) bool {
		return slices.ContainsFunc(dels, func(d ast.DeclAny) bool { return d.Span().Start <= offset && offset < d.Span().End })
	}
	// LeafSpan, because the Span of a brace pair covers its whole body.
	line := func(tok token.Token) int { return tok.LeafSpan().StartLoc().Line }
	lastLine := func(c token.Token) int {
		return line(c) + strings.Count(strings.TrimRight(c.Text(), "\n"), "\n")
	}
	for _, d := range dels {
		var gap []token.Token
		var next token.Token
		found := false
		for tok := range f.AST().Stream().All() {
			switch {
			case tok.LeafSpan().Start < d.Span().End:
			case tok.Kind() == token.Comment:
				gap = append(gap, tok)
			case tok.Kind().IsSkippable():
			default:
				next, found = tok, true
			}
			if found {
				break
			}
		}
		if found && removed(next.LeafSpan().Start) {
			continue // these belong to the next removed declaration and are checked through it
		}
		doc := len(gap)
		if found && !closesScope(next) {
			for above := line(next); doc > 0 && lastLine(gap[doc-1]) == above-1; doc-- {
				above = line(gap[doc-1])
			}
		}
		for _, c := range gap[:doc] {
			if line(c) != d.Span().EndLoc().Line {
				return fmt.Errorf("%s:%d: comment %q after a removed draft declaration would survive in the stable output; move it directly above a declaration, or delete it", f.Path(), line(c), strings.TrimSpace(c.Text()))
			}
		}
	}
	return nil
}

// closesScope reports whether tok is the closing token of a pair such as "}"; Text
// returns the open token's text for both ends.
func closesScope(tok token.Token) bool {
	if tok.IsLeaf() {
		return false
	}
	_, end := tok.StartEnd()
	return end == tok
}

// check blames the comment with the largest share of surplus words, since a deleted
// comment can share words with a surviving one.
func (d draftComments) check(stable *token.Stream) error {
	out := commentWords(stable)
	blamed, most := -1, 0.0
	for i, c := range d.comments {
		words := strings.Fields(c.text)
		over := len(slices.DeleteFunc(slices.Clone(words), func(w string) bool { return out[w] <= d.allowed[w] }))
		if share := float64(over) / float64(len(words)); share > most {
			blamed, most = i, share
		}
	}
	if blamed < 0 {
		return nil
	}
	return fmt.Errorf("%s: comment %q of a removed draft declaration survives in the stable output; move it directly above the draft declaration, or delete it", d.comments[blamed].at, d.comments[blamed].text)
}

// commentWords counts comment words after stripping the delimiters and block-comment
// margins that protoc strips from SourceCodeInfo comments.
func commentWords(stream *token.Stream) map[string]int {
	counts := map[string]int{}
	for tok := range stream.All() {
		if tok.Kind() != token.Comment {
			continue
		}
		text, block := strings.CutPrefix(tok.Text(), "/*")
		if block {
			text = strings.TrimSuffix(text, "*/")
		} else {
			text = strings.TrimPrefix(text, "//")
		}
		for i, line := range strings.Split(text, "\n") {
			if block && i > 0 {
				line = strings.TrimPrefix(strings.TrimSpace(line), "*")
			}
			for _, w := range strings.Fields(line) {
				counts[w]++
			}
		}
	}
	return counts
}
