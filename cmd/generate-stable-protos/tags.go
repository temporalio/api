package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/bufbuild/protocompile/experimental/ir"
	"github.com/bufbuild/protocompile/experimental/seq"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	draftFieldName   ir.FullName = "temporal.api.protometa.v1.draft"
	draftMessageName ir.FullName = "temporal.api.protometa.v1.draft_message"
)

type draftTags map[ir.FullName]struct{}

func getDraftTags(file *ir.File) (draftTags, error) {
	tags := make(draftTags)
	extendees := map[string]string{
		"draft": "FieldOptions", "draft_message": "MessageOptions",
		"draft_enum": "EnumOptions", "draft_enum_value": "EnumValueOptions",
		"draft_service": "ServiceOptions", "draft_method": "MethodOptions",
		"draft_file": "FileOptions", "draft_oneof": "OneofOptions",
		"draft_extension": "FieldOptions",
	}
	for ext := range seq.Values(file.AllExtensions()) {
		name, known := extendees[ext.Name()]
		if !known || ext.FullName() != ir.FullName("temporal.api.protometa.v1."+ext.Name()) {
			continue
		}
		if ext.Container().FullName() != ir.FullName("google.protobuf."+name) || ext.FDPType() != descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.IsRepeated() {
			return nil, fmt.Errorf("invalid draft option schema for %s: want singular string extending google.protobuf.%s", ext.FullName(), name)
		}
		tags[ext.FullName()] = struct{}{}
	}
	if _, ok := tags[draftFieldName]; !ok {
		return nil, fmt.Errorf("%s not found in %s", draftFieldName, draftTagsPath)
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

func validateDraftAnnotations(tags draftTags, f *ir.File) error {
	check := func(name string, placement string, opts ir.MessageValue) error {
		for v := range opts.Fields() {
			if _, ok := tags[v.Field().FullName()]; !ok {
				continue
			}
			if !((placement == "field" && v.Field().FullName() == draftFieldName) ||
				(placement == "message" && v.Field().FullName() == draftMessageName)) {
				return fmt.Errorf("%s: unsupported draft annotation on %s", name, placement)
			}
			feature, ok := v.AsString()
			if !ok || feature == "" || strings.ContainsFunc(feature, unicode.IsSpace) {
				return fmt.Errorf("%s: draft feature must be nonempty and contain no whitespace", name)
			}
		}
		return nil
	}
	if err := check(f.Path(), "file", f.Options()); err != nil {
		return err
	}
	for t := range seq.Values(f.AllTypes()) {
		placement := "message"
		if t.IsEnum() {
			placement = "enum"
		}
		if err := check(string(t.FullName()), placement, t.Options()); err != nil {
			return err
		}
		for m := range seq.Values(t.Members()) {
			placement := "field"
			if m.IsEnumValue() {
				placement = "enum value"
			}
			if err := check(string(m.FullName()), placement, m.Options()); err != nil {
				return err
			}
		}
		for o := range seq.Values(t.Oneofs()) {
			if err := check(string(o.FullName()), "oneof", o.Options()); err != nil {
				return err
			}
		}
	}
	for ext := range seq.Values(f.AllExtensions()) {
		if err := check(string(ext.FullName()), "extension", ext.Options()); err != nil {
			return err
		}
		for _, t := range []ir.Type{ext.Element(), ext.Container(), ext.Parent()} {
			if isDraftType(tags, t) {
				return fmt.Errorf("%s: extension references draft type %s unsupported by omission", ext.FullName(), t.FullName())
			}
		}
	}
	for s := range seq.Values(f.Services()) {
		if err := check(string(s.FullName()), "service", s.Options()); err != nil {
			return err
		}
		for m := range seq.Values(s.Methods()) {
			if err := check(string(m.FullName()), "method", m.Options()); err != nil {
				return err
			}
			input, _ := m.Input()
			output, _ := m.Output()
			for _, t := range []ir.Type{input, output} {
				if isDraftType(tags, t) {
					return fmt.Errorf("%s: RPC references draft type %s unavailable in the stable API", m.FullName(), t.FullName())
				}
			}
		}
	}
	return nil
}

func isDraftType(tags draftTags, t ir.Type) bool {
	if t.IsZero() || t.IsPredeclared() {
		return false
	}
	return tags.has(t.Options()) || isDraftType(tags, t.Parent())
}

func isDraftMember(tags draftTags, m ir.Member) bool {
	if tags.has(m.Options()) {
		return true
	}
	t := m.Element()
	if m.IsMap() {
		_, value := t.EntryFields()
		t = value.Element()
	}
	return isDraftType(tags, t)
}
