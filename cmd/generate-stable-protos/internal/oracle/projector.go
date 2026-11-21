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
	messageFieldsTag    = 2
	messageNestedTag    = 3
	messageOneofsTag    = 8
)

// projector projects draft declarations out of protoc-compiled descriptors.
type projector struct {
	cfg                  Config
	fieldExt, messageExt protowire.Number
	// draftTypes holds the fully qualified names (".pkg.Msg") of draft messages and of
	// every type nested in one.
	draftTypes map[string]bool
	mapEntries map[string]*descriptorpb.DescriptorProto
}

func newProjector(cfg Config, set *descriptorpb.FileDescriptorSet) (*projector, error) {
	p := &projector{cfg: cfg, draftTypes: map[string]bool{}, mapEntries: map[string]*descriptorpb.DescriptorProto{}}
	for _, fd := range set.GetFile() {
		if fd.GetName() != cfg.DraftFile {
			continue
		}
		for _, ext := range fd.GetExtension() {
			switch {
			case ext.GetName() == "draft" && ext.GetExtendee() == ".google.protobuf.FieldOptions":
				p.fieldExt = protowire.Number(ext.GetNumber())
			case ext.GetName() == "draft_message" && ext.GetExtendee() == ".google.protobuf.MessageOptions":
				p.messageExt = protowire.Number(ext.GetNumber())
			}
		}
	}
	if p.fieldExt == 0 || p.messageExt == 0 {
		return nil, fmt.Errorf("draft and draft_message extensions not found in %s", cfg.DraftFile)
	}
	for _, fd := range set.GetFile() {
		p.collectTypes(packagePrefix(fd), fd.GetMessageType(), false)
	}
	return p, nil
}

func (p *projector) collectTypes(prefix string, msgs []*descriptorpb.DescriptorProto, parentDraft bool) {
	for _, m := range msgs {
		name := prefix + "." + m.GetName()
		draft := parentDraft || hasOption(m.GetOptions(), p.messageExt)
		if draft {
			p.draftTypes[name] = true
			for _, e := range m.GetEnumType() {
				p.draftTypes[name+"."+e.GetName()] = true
			}
		}
		if m.GetOptions().GetMapEntry() {
			p.mapEntries[name] = m
		}
		p.collectTypes(name, m.GetNestedType(), draft)
	}
}

func (p *projector) isDraftField(f *descriptorpb.FieldDescriptorProto) bool {
	if hasOption(f.GetOptions(), p.fieldExt) || p.draftTypes[f.GetTypeName()] {
		return true
	}
	entry := p.mapEntries[f.GetTypeName()]
	return entry != nil && p.draftTypes[entry.GetField()[1].GetTypeName()]
}

// projectFile returns fd without its draft declarations, and the source paths of the
// outermost declarations it removed.
func (p *projector) projectFile(fd *descriptorpb.FileDescriptorProto) (*descriptorpb.FileDescriptorProto, [][]int32) {
	fd = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	fd.Name = new(p.cfg.stablePath(fd.GetName()))
	prefix := packagePrefix(fd)
	var msgs []*descriptorpb.DescriptorProto
	var removed [][]int32
	for i, m := range fd.GetMessageType() {
		path := []int32{fileMessagesTag, int32(i)}
		if p.draftTypes[prefix+"."+m.GetName()] {
			removed = append(removed, path)
			continue
		}
		msgs = append(msgs, p.projectMessage(prefix, m, path, &removed))
	}
	fd.MessageType = msgs
	return normalize(fd), removed
}

func (p *projector) projectMessage(prefix string, m *descriptorpb.DescriptorProto, path []int32, removed *[][]int32) *descriptorpb.DescriptorProto {
	name := prefix + "." + m.GetName()
	var fields []*descriptorpb.FieldDescriptorProto
	var draftFields []int
	droppedEntries := map[string]bool{}
	kept := map[int32]int{}
	for i, f := range m.GetField() {
		if p.isDraftField(f) {
			if p.mapEntries[f.GetTypeName()] != nil {
				droppedEntries[f.GetTypeName()] = true
			}
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
	removedOneofs := map[int32]bool{}
	for i, od := range m.GetOneofDecl() {
		if kept[int32(i)] > 0 {
			remap[int32(i)] = int32(len(oneofs))
			oneofs = append(oneofs, od)
		} else if !isSyntheticOneof(m, int32(i)) {
			// A synthetic oneof has no source of its own; its field is recorded below.
			removedOneofs[int32(i)] = true
			*removed = append(*removed, slices.Concat(path, []int32{messageOneofsTag, int32(i)}))
		}
	}
	for _, i := range draftFields {
		// A removed oneof's span already covers its fields.
		if f := m.GetField()[i]; f.OneofIndex == nil || !removedOneofs[f.GetOneofIndex()] {
			*removed = append(*removed, slices.Concat(path, []int32{messageFieldsTag, int32(i)}))
		}
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
		case p.draftTypes[nestedName]:
			*removed = append(*removed, nestedPath)
		case droppedEntries[nestedName]:
			// A map entry is synthetic and has no source; its field is recorded above.
		default:
			nested = append(nested, p.projectMessage(name, n, nestedPath, removed))
		}
	}
	m.Field, m.OneofDecl, m.NestedType = fields, oneofs, nested
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
	return fd
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
