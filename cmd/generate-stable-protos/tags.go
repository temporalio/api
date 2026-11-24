package main

import (
	"cmp"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/bufbuild/protocompile/experimental/ir"
	"github.com/bufbuild/protocompile/experimental/seq"
	"google.golang.org/protobuf/types/descriptorpb"
)

type draftTags struct {
	packageName ir.FullName
	names       map[ir.FullName]struct{}
}

func getDraftTags(file *ir.File) (draftTags, error) {
	tags := draftTags{packageName: file.Package(), names: make(map[ir.FullName]struct{})}
	extendees := map[string]string{
		"draft":            "FieldOptions",
		"draft_message":    "MessageOptions",
		"draft_enum":       "EnumOptions",
		"draft_enum_value": "EnumValueOptions",
		"draft_service":    "ServiceOptions",
		"draft_method":     "MethodOptions",
		"draft_oneof":      "OneofOptions",
	}
	for ext := range seq.Values(file.AllExtensions()) {
		name, known := extendees[ext.Name()]
		if !known || ext.FullName() != tags.packageName.Append(ext.Name()) {
			continue
		}
		if ext.Container().FullName() != ir.FullName("google.protobuf."+name) || ext.FDPType() != descriptorpb.FieldDescriptorProto_TYPE_STRING || ext.IsRepeated() {
			return draftTags{}, fmt.Errorf("invalid draft option schema for %s: want singular string extending google.protobuf.%s", ext.FullName(), name)
		}
		tags.names[ext.FullName()] = struct{}{}
	}
	if name := tags.packageName.Append("draft"); !tags.contains(name) {
		return draftTags{}, fmt.Errorf("%s not found in %s", name, file.Path())
	}
	return tags, nil
}

func (tags draftTags) contains(name ir.FullName) bool {
	_, ok := tags.names[name]
	return ok
}

func (tags draftTags) annotation(opts ir.MessageValue) ir.Value {
	for v := range opts.Fields() {
		if tags.contains(v.Field().FullName()) {
			return v
		}
	}
	return ir.Value{}
}

func (tags draftTags) has(opts ir.MessageValue) bool {
	return !tags.annotation(opts).IsZero()
}

func (tags draftTags) feature(opts ir.MessageValue) draftFeature {
	feature, _ := tags.annotation(opts).AsString()
	return feature
}

type draftAnnotations map[ir.FullName]draftFeature

type draftFeature = string

// validateDraftAnnotations checks annotation placement, feature names, and enum values in retained options.
func validateDraftAnnotations(tags draftTags, f *ir.File, projected bool) (draftAnnotations, error) {
	accepted := draftAnnotations{}
	check := func(name ir.FullName, placement, option string, opts ir.MessageValue, draftParent bool) error {
		v := tags.annotation(opts)
		if v.IsZero() {
			if projected && !draftParent {
				for opt := range opts.Fields() {
					if value := draftOptionEnumValue(tags, opt); value != "" {
						return fmt.Errorf("%s: option %s references draft enum value %s", name, opt.Field().FullName(), value)
					}
				}
			}
			return nil
		}
		if option == "" || v.Field().FullName() != tags.packageName.Append(option) {
			return fmt.Errorf("%s: unsupported draft annotation on %s", name, placement)
		}
		feature, ok := v.AsString()
		if !ok || feature == "" || strings.ContainsFunc(feature, unicode.IsSpace) {
			return fmt.Errorf("%s: draft feature must be nonempty and contain no whitespace", name)
		}
		accepted[name] = feature
		return nil
	}
	if err := check(ir.FullName(f.Path()), "file", "", f.Options(), false); err != nil {
		return nil, err
	}
	for t := range seq.Values(f.AllTypes()) {
		placement, option := "message", "draft_message"
		if t.IsEnum() {
			placement, option = "enum", "draft_enum"
		}
		if err := check(t.FullName(), placement, option, t.Options(), tags.typeFeature(t.Parent()) != ""); err != nil {
			return nil, err
		}
		draft := tags.typeFeature(t) != ""
		for m := range seq.Values(t.Members()) {
			placement, option := "field", "draft"
			if m.IsEnumValue() {
				placement, option = "enum value", "draft_enum_value"
			}
			if err := check(m.FullName(), placement, option, m.Options(), draft || tags.has(m.Oneof().Options())); err != nil {
				return nil, err
			}
		}
		for o := range seq.Values(t.Oneofs()) {
			if err := check(o.FullName(), "oneof", "draft_oneof", o.Options(), draft); err != nil {
				return nil, err
			}
		}
	}
	for ext := range seq.Values(f.AllExtensions()) {
		if err := check(ext.FullName(), "extension", "", ext.Options(), tags.typeFeature(ext.Parent()) != ""); err != nil {
			return nil, err
		}
	}
	for s := range seq.Values(f.Services()) {
		if err := check(s.FullName(), "service", "draft_service", s.Options(), false); err != nil {
			return nil, err
		}
		for m := range seq.Values(s.Methods()) {
			if err := check(m.FullName(), "method", "draft_method", m.Options(), tags.has(s.Options())); err != nil {
				return nil, err
			}
		}
	}
	return accepted, nil
}

func draftOptionEnumValue(tags draftTags, option ir.Value) ir.FullName {
	for element := range seq.Values(option.Elements()) {
		if value := element.AsEnum(); !value.IsZero() && (tags.has(value.Options()) || tags.typeFeature(value.Parent()) != "") {
			return value.FullName()
		}
		for field := range element.AsMessage().Fields() {
			if value := draftOptionEnumValue(tags, field); value != "" {
				return value
			}
		}
	}
	return ""
}

const fieldPathPattern = `([a-zA-Z_][a-zA-Z_0-9]*(?:\.[a-zA-Z_][a-zA-Z_0-9]*)*)`

var (
	httpPathVariable = regexp.MustCompile(`\{` + fieldPathPattern + `(?:=[^{}]+)?\}`)
	headerVariable   = regexp.MustCompile(`\{` + fieldPathPattern + `\}`)
)

func validateMethodFieldReferences(tags draftTags, method ir.Method, input, output ir.Type) error {
	for option := range method.Options().Fields() {
		switch option.Field().FullName() {
		case "google.api.http":
			if path := draftHTTPField(tags, option.AsMessage(), input, output); path != "" {
				return fmt.Errorf("%s: http rule references draft field %s", method.FullName(), path)
			}
		case "temporal.api.protometa.v1.request_header":
			for header := range seq.Values(option.Elements()) {
				template := messageString(header.AsMessage(), "value")
				if path := draftTemplateField(tags, input, template, headerVariable); path != "" {
					return fmt.Errorf("%s: request header references draft field %s", method.FullName(), path)
				}
			}
		}
	}
	return nil
}

func draftHTTPField(tags draftTags, rule ir.MessageValue, input, output ir.Type) string {
	for field := range rule.Fields() {
		var draftPath string
		switch field.Field().Name() {
		case "get", "put", "post", "delete", "patch":
			template, _ := field.AsString()
			draftPath = draftTemplateField(tags, input, template, httpPathVariable)
		case "custom":
			template := messageString(field.AsMessage(), "path")
			draftPath = draftTemplateField(tags, input, template, httpPathVariable)
		case "body":
			path, _ := field.AsString()
			if referencesDraftField(tags, input, path) {
				draftPath = path
			}
		case "response_body":
			path, _ := field.AsString()
			if referencesDraftField(tags, output, path) {
				draftPath = path
			}
		case "additional_bindings":
			for binding := range seq.Values(field.Elements()) {
				if path := draftHTTPField(tags, binding.AsMessage(), input, output); path != "" {
					return path
				}
			}
		}
		if draftPath != "" {
			return draftPath
		}
	}
	return ""
}

func messageString(value ir.MessageValue, name string) string {
	text, _ := value.Field(value.Type().MemberByName(name)).AsString()
	return text
}

func draftTemplateField(tags draftTags, message ir.Type, template string, variable *regexp.Regexp) string {
	for _, match := range variable.FindAllStringSubmatch(template, -1) {
		path := match[1]
		if referencesDraftField(tags, message, path) {
			return path
		}
	}
	return ""
}

func referencesDraftField(tags draftTags, message ir.Type, path string) bool {
	for name := range strings.SplitSeq(path, ".") {
		if !message.IsMessage() {
			return false
		}
		field := message.MemberByName(name)
		if field.IsZero() {
			return false
		}
		if tags.has(field.Options()) || tags.has(field.Oneof().Options()) {
			return true
		}
		message = field.Element()
	}
	return false
}

func (tags draftTags) typeFeature(t ir.Type) draftFeature {
	for ; !t.IsZero() && !t.IsPredeclared(); t = t.Parent() {
		if feature := tags.feature(t.Options()); feature != "" {
			return feature
		}
	}
	return ""
}

func (tags draftTags) memberContainerFeature(field ir.Member, parent draftFeature) draftFeature {
	return cmp.Or(tags.feature(field.Oneof().Options()), parent)
}

func (tags draftTags) memberFeature(member ir.Member, parent draftFeature) draftFeature {
	return cmp.Or(tags.feature(member.Options()), tags.memberContainerFeature(member, parent))
}

func (tags draftTags) collectGates(file *ir.File, gates draftAnnotations) {
	add := func(name ir.FullName, gate draftFeature) {
		if gate != "" {
			gates[name] = gate
		}
	}
	for typ := range seq.Values(file.AllTypes()) {
		if typ.IsMapEntry() {
			continue
		}
		parent := tags.typeFeature(typ)
		add(typ.FullName(), parent)
		for member := range seq.Values(typ.Members()) {
			add(member.FullName(), tags.memberFeature(member, parent))
		}
		for oneof := range seq.Values(typ.Oneofs()) {
			if !oneof.AST().IsZero() {
				add(oneof.FullName(), cmp.Or(tags.feature(oneof.Options()), parent))
			}
		}
	}
	for service := range seq.Values(file.Services()) {
		parent := tags.feature(service.Options())
		add(service.FullName(), parent)
		for method := range seq.Values(service.Methods()) {
			add(method.FullName(), cmp.Or(tags.feature(method.Options()), parent))
		}
	}
}
