package main

import (
	"fmt"

	"github.com/bufbuild/protocompile/experimental/ir"
	"github.com/bufbuild/protocompile/experimental/seq"
)

// draftTags is the set of fully-qualified extension names that mark a
// declaration as experimental/draft.
//
// The set is discovered from temporal/api_next/protometa/v1/experimental.proto
// (see [getDraftTags]) rather than hardcoded.
type draftTags map[ir.FullName]struct{}

func getDraftTags(experimentalFile *ir.File) (draftTags, error) {
	tags := make(draftTags)
	for ext := range seq.Values(experimentalFile.Extensions()) {
		tags[ext.FullName()] = struct{}{}
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf(
			"no draft extensions found in %s, ensure path is set correctly",
			experimentalTagsPath,
		)
	}
	return tags, nil
}

func (tags draftTags) has(opts ir.MessageValue) bool {
	for v := range opts.Fields() {
		if _, ok := tags[v.Field().FullName()]; ok {
			return true
		}
	}
	return false
}

func isDraftType(tags draftTags, t ir.Type) bool {
	return tags.has(t.Options())
}

func isDraftMember(tags draftTags, m ir.Member) bool {
	return tags.has(m.Options())
}

func isDraftService(tags draftTags, s ir.Service) bool {
	return tags.has(s.Options())
}

func isDraftMethod(tags draftTags, m ir.Method) bool {
	return tags.has(m.Options())
}
