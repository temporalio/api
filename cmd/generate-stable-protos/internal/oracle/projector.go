package oracle

import (
	"fmt"
	"slices"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Source paths, per the field numbers in descriptor.proto.
const (
	fileDependenciesTag = 3
	fileMessagesTag     = 4
	fileEnumsTag        = 5
	fileServicesTag     = 6
	fileSyntaxTag       = 12
	messageFieldsTag    = 2
	messageNestedTag    = 3
	messageEnumsTag     = 4
	messageOneofsTag    = 8
	enumValuesTag       = 2
	serviceMethodsTag   = 2
)

// projector projects draft declarations out of protoc-compiled descriptors.
type projector struct {
	cfg                                                   Config
	fieldExt, messageExt, enumExt, enumValueExt, oneofExt protowire.Number
	serviceExt, methodExt                                 protowire.Number
}

func newProjector(cfg Config, set *descriptorpb.FileDescriptorSet) (*projector, error) {
	p := &projector{cfg: cfg}
	type extension struct{ name, extendee string }
	numbers := map[extension]*protowire.Number{
		{"draft", ".google.protobuf.FieldOptions"}:                &p.fieldExt,
		{"draft_message", ".google.protobuf.MessageOptions"}:      &p.messageExt,
		{"draft_enum", ".google.protobuf.EnumOptions"}:            &p.enumExt,
		{"draft_enum_value", ".google.protobuf.EnumValueOptions"}: &p.enumValueExt,
		{"draft_oneof", ".google.protobuf.OneofOptions"}:          &p.oneofExt,
		{"draft_service", ".google.protobuf.ServiceOptions"}:      &p.serviceExt,
		{"draft_method", ".google.protobuf.MethodOptions"}:        &p.methodExt,
	}
	for _, fd := range set.GetFile() {
		if fd.GetName() != cfg.DraftFile {
			continue
		}
		for _, ext := range fd.GetExtension() {
			if num := numbers[extension{ext.GetName(), ext.GetExtendee()}]; num != nil {
				*num = protowire.Number(ext.GetNumber())
			}
		}
	}
	if p.fieldExt == 0 || p.messageExt == 0 {
		return nil, fmt.Errorf("draft and draft_message extensions not found in %s", cfg.DraftFile)
	}
	return p, nil
}

// projectFile returns fd without its draft declarations, and the source paths of the
// declarations it removed.
func (p *projector) projectFile(fd *descriptorpb.FileDescriptorProto) (*descriptorpb.FileDescriptorProto, [][]int32) {
	fd = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	fd.Name = new(p.cfg.stablePath(fd.GetName()))
	prefix := packagePrefix(fd)
	var msgs []*descriptorpb.DescriptorProto
	var removed [][]int32
	for i, m := range fd.GetMessageType() {
		path := []int32{fileMessagesTag, int32(i)}
		if hasOption(m.GetOptions(), p.messageExt) {
			removed = append(removed, path)
			continue
		}
		msgs = append(msgs, p.projectMessage(prefix, m, path, &removed))
	}
	fd.MessageType = msgs
	fd.EnumType = p.projectEnums(fd.GetEnumType(), []int32{fileEnumsTag}, &removed)
	var services []*descriptorpb.ServiceDescriptorProto
	for i, s := range fd.GetService() {
		path := []int32{fileServicesTag, int32(i)}
		if hasOption(s.GetOptions(), p.serviceExt) {
			removed = append(removed, path)
			continue
		}
		var methods []*descriptorpb.MethodDescriptorProto
		for j, m := range s.GetMethod() {
			if hasOption(m.GetOptions(), p.methodExt) {
				removed = append(removed, slices.Concat(path, []int32{serviceMethodsTag, int32(j)}))
				continue
			}
			methods = append(methods, m)
		}
		s.Method = methods
		services = append(services, s)
	}
	fd.Service = services
	return normalize(fd), removed
}

func (p *projector) projectEnums(enums []*descriptorpb.EnumDescriptorProto, path []int32, removed *[][]int32) []*descriptorpb.EnumDescriptorProto {
	var stable []*descriptorpb.EnumDescriptorProto
	for i, e := range enums {
		enumPath := slices.Concat(path, []int32{int32(i)})
		if hasOption(e.GetOptions(), p.enumExt) {
			*removed = append(*removed, enumPath)
			continue
		}
		var values []*descriptorpb.EnumValueDescriptorProto
		for j, v := range e.GetValue() {
			if hasOption(v.GetOptions(), p.enumValueExt) {
				*removed = append(*removed, slices.Concat(enumPath, []int32{enumValuesTag, int32(j)}))
				continue
			}
			values = append(values, v)
		}
		e.Value = values
		stable = append(stable, e)
	}
	return stable
}

func (p *projector) projectMessage(prefix string, m *descriptorpb.DescriptorProto, path []int32, removed *[][]int32) *descriptorpb.DescriptorProto {
	name := prefix + "." + m.GetName()
	var fields []*descriptorpb.FieldDescriptorProto
	var draftFields []int
	droppedTypes := map[string]bool{}
	kept := map[int32]int{}
	for i, f := range m.GetField() {
		draftOneof := f.OneofIndex != nil && hasOption(m.GetOneofDecl()[f.GetOneofIndex()].GetOptions(), p.oneofExt)
		if draftOneof || hasOption(f.GetOptions(), p.fieldExt) {
			droppedTypes[f.GetTypeName()] = true
			draftFields = append(draftFields, i)
			continue
		}
		if f.OneofIndex != nil {
			kept[f.GetOneofIndex()]++
		}
		fields = append(fields, f)
	}
	// A oneof whose branches are all draft goes too, including proto3 optional's
	// synthetic oneof; the remaining oneof indexes shift down.
	remap := map[int32]int32{}
	var oneofs []*descriptorpb.OneofDescriptorProto
	for i, od := range m.GetOneofDecl() {
		if kept[int32(i)] > 0 {
			remap[int32(i)] = int32(len(oneofs))
			oneofs = append(oneofs, od)
		} else if !isSyntheticOneof(m, int32(i)) {
			// A synthetic oneof has no source of its own; its field is recorded below.
			*removed = append(*removed, slices.Concat(path, []int32{messageOneofsTag, int32(i)}))
		}
	}
	for _, i := range draftFields {
		// Record fields for path remapping even when a removed oneof's span already covers them.
		*removed = append(*removed, slices.Concat(path, []int32{messageFieldsTag, int32(i)}))
	}
	for _, f := range fields {
		if f.OneofIndex != nil {
			f.OneofIndex = new(remap[f.GetOneofIndex()])
		}
	}
	var nested []*descriptorpb.DescriptorProto
	for i, n := range m.GetNestedType() {
		nestedName := name + "." + n.GetName()
		nestedPath := slices.Concat(path, []int32{messageNestedTag, int32(i)})
		switch {
		case hasOption(n.GetOptions(), p.messageExt):
			*removed = append(*removed, nestedPath)
		case n.GetOptions().GetMapEntry() && droppedTypes[nestedName]:
			// A map entry is synthetic and has no source; its field is recorded above.
			*removed = append(*removed, nestedPath)
		default:
			nested = append(nested, p.projectMessage(name, n, nestedPath, removed))
		}
	}
	m.Field, m.OneofDecl, m.NestedType = fields, oneofs, nested
	m.EnumType = p.projectEnums(m.GetEnumType(), slices.Concat(path, []int32{messageEnumsTag}), removed)
	return m
}

// isSyntheticOneof reports whether oneof i only exists for a proto3 optional field.
func isSyntheticOneof(m *descriptorpb.DescriptorProto, i int32) bool {
	for _, f := range m.GetField() {
		if f.OneofIndex != nil && f.GetOneofIndex() == i && !f.GetProto3Optional() {
			return false
		}
	}
	return true
}

// normalize drops what the oracle does not compare: source info, which holds positions
// and comments, and the import lists, which Check checks separately.
func normalize(fd *descriptorpb.FileDescriptorProto) *descriptorpb.FileDescriptorProto {
	fd = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	fd.SourceCodeInfo = nil
	fd.Dependency, fd.PublicDependency, fd.WeakDependency = nil, nil, nil
	normalizeSyntheticOneofNames(fd.GetMessageType())
	return fd
}

func normalizeSyntheticOneofNames(messages []*descriptorpb.DescriptorProto) {
	for _, message := range messages {
		for _, field := range message.GetField() {
			if field.GetProto3Optional() && field.OneofIndex != nil {
				message.OneofDecl[field.GetOneofIndex()].Name = new(field.GetName())
			}
		}
		normalizeSyntheticOneofNames(message.GetNestedType())
	}
}

// hasOption reports whether opts sets the extension num. The draft extensions are not
// registered with the Go runtime, so their values stay in the unknown fields.
func hasOption(opts proto.Message, num protowire.Number) bool {
	if opts == nil || !opts.ProtoReflect().IsValid() {
		return false
	}
	b := opts.ProtoReflect().GetUnknown()
	for len(b) > 0 {
		n, typ, size := protowire.ConsumeTag(b)
		if size < 0 {
			return false
		}
		b = b[size:]
		if n == num {
			return true
		}
		size = protowire.ConsumeFieldValue(n, typ, b)
		if size < 0 {
			return false
		}
		b = b[size:]
	}
	return false
}

func packagePrefix(fd *descriptorpb.FileDescriptorProto) string {
	if fd.GetPackage() == "" {
		return ""
	}
	return "." + fd.GetPackage()
}
