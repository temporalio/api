package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestStableProjectionOracle compares the generator against an independent reference:
// protoc compiles the api_next input and the generated stable output, and the stable
// descriptors must equal the input descriptors with the draft declarations projected out.
// The projection works on descriptors, where removing a declaration is simple, so it
// shares no logic with the generator's AST editing.
func TestStableProjectionOracle(t *testing.T) {
	t.Parallel()
	for _, fixture := range oracleFixtures(t) {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			checkAgainstOracle(t, filepath.Join("testdata", fixture, "input"))
		})
	}
}

// oracleFixtures lists every fixture the generator is expected to accept.
func oracleFixtures(t *testing.T) []string {
	t.Helper()
	fixtures := []string{"imports", "comments"}
	dirs, err := os.ReadDir("testdata/classification")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		data, err := os.ReadFile(filepath.Join("testdata/classification", dir.Name(), "expected.json"))
		if err != nil {
			t.Fatal(err)
		}
		var expected struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &expected); err != nil {
			t.Fatal(err)
		}
		if expected.Error == "" {
			fixtures = append(fixtures, filepath.Join("classification", dir.Name()))
		}
	}
	return fixtures
}

func checkAgainstOracle(t *testing.T, root string) {
	t.Helper()
	out := t.TempDir()
	if err := stableProtosGenerator(t.Context(), root, out); err != nil {
		t.Fatal(err)
	}
	input, _ := protocSet(t, []string{root}, protoFiles(t, root, apiNextPrefix))
	// The output directory comes first so it shadows any stable tree checked in under root.
	output, warnings := protocSet(t, []string{out, root}, protoFiles(t, out, stableAPIPrefix))
	if strings.Contains(warnings, "is unused") {
		t.Fatalf("stable output has unused imports:\n%s", warnings)
	}

	oracle := newDraftOracle(t, input)
	expected := map[string]*descriptorpb.FileDescriptorProto{}
	inputDeps := map[string]bool{}
	for _, fd := range input.GetFile() {
		if fd.GetName() == draftTagsPath {
			continue
		}
		projected := oracle.projectFile(fd)
		expected[projected.GetName()] = projected
		for _, dep := range fd.GetDependency() {
			inputDeps[oracleStablePath(dep)] = true
		}
	}
	actual := map[string]*descriptorpb.FileDescriptorProto{}
	for _, fd := range output.GetFile() {
		// Import pruning needs cross-file usage analysis, which would re-implement the
		// generator; the unused-import warning check above covers it instead.
		for _, dep := range fd.GetDependency() {
			if !inputDeps[dep] {
				t.Errorf("%s: import %s is not an import of the input", fd.GetName(), dep)
			}
		}
		actual[fd.GetName()] = normalizeForOracle(fd)
	}
	if diff := cmp.Diff(expected, actual, protocmp.Transform()); diff != "" {
		t.Fatalf("stable descriptors differ from the oracle projection (-expected +actual):\n%s", diff)
	}
}

// draftOracle projects draft declarations out of protoc-compiled descriptors.
type draftOracle struct {
	fieldExt, messageExt protowire.Number
	// draftTypes holds the fully qualified names (".pkg.Msg") of draft messages and of
	// every type nested in one.
	draftTypes map[string]bool
	mapEntries map[string]*descriptorpb.DescriptorProto
}

func newDraftOracle(t *testing.T, set *descriptorpb.FileDescriptorSet) *draftOracle {
	t.Helper()
	o := &draftOracle{draftTypes: map[string]bool{}, mapEntries: map[string]*descriptorpb.DescriptorProto{}}
	for _, fd := range set.GetFile() {
		if fd.GetName() != draftTagsPath {
			continue
		}
		for _, ext := range fd.GetExtension() {
			switch {
			case ext.GetName() == "draft" && ext.GetExtendee() == ".google.protobuf.FieldOptions":
				o.fieldExt = protowire.Number(ext.GetNumber())
			case ext.GetName() == "draft_message" && ext.GetExtendee() == ".google.protobuf.MessageOptions":
				o.messageExt = protowire.Number(ext.GetNumber())
			}
		}
	}
	if o.fieldExt == 0 || o.messageExt == 0 {
		t.Fatalf("draft and draft_message extensions not found in %s", draftTagsPath)
	}
	for _, fd := range set.GetFile() {
		o.collectTypes(packagePrefix(fd), fd.GetMessageType(), false)
	}
	return o
}

func (o *draftOracle) collectTypes(prefix string, msgs []*descriptorpb.DescriptorProto, parentDraft bool) {
	for _, m := range msgs {
		name := prefix + "." + m.GetName()
		draft := parentDraft || hasOption(m.GetOptions(), o.messageExt)
		if draft {
			o.draftTypes[name] = true
			for _, e := range m.GetEnumType() {
				o.draftTypes[name+"."+e.GetName()] = true
			}
		}
		if m.GetOptions().GetMapEntry() {
			o.mapEntries[name] = m
		}
		o.collectTypes(name, m.GetNestedType(), draft)
	}
}

func (o *draftOracle) isDraftField(f *descriptorpb.FieldDescriptorProto) bool {
	if hasOption(f.GetOptions(), o.fieldExt) || o.draftTypes[f.GetTypeName()] {
		return true
	}
	entry := o.mapEntries[f.GetTypeName()]
	return entry != nil && o.draftTypes[entry.GetField()[1].GetTypeName()]
}

func (o *draftOracle) projectFile(fd *descriptorpb.FileDescriptorProto) *descriptorpb.FileDescriptorProto {
	fd = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	fd.Name = new(oracleStablePath(fd.GetName()))
	prefix := packagePrefix(fd)
	var msgs []*descriptorpb.DescriptorProto
	for _, m := range fd.GetMessageType() {
		if !o.draftTypes[prefix+"."+m.GetName()] {
			msgs = append(msgs, o.projectMessage(prefix, m))
		}
	}
	fd.MessageType = msgs
	return normalizeForOracle(fd)
}

func (o *draftOracle) projectMessage(prefix string, m *descriptorpb.DescriptorProto) *descriptorpb.DescriptorProto {
	name := prefix + "." + m.GetName()
	var fields []*descriptorpb.FieldDescriptorProto
	droppedEntries := map[string]bool{}
	kept := map[int32]int{}
	for _, f := range m.GetField() {
		if o.isDraftField(f) {
			if o.mapEntries[f.GetTypeName()] != nil {
				droppedEntries[f.GetTypeName()] = true
			}
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
		}
	}
	for _, f := range fields {
		if f.OneofIndex != nil {
			f.OneofIndex = new(remap[f.GetOneofIndex()])
		}
	}
	var nested []*descriptorpb.DescriptorProto
	for _, n := range m.GetNestedType() {
		nestedName := name + "." + n.GetName()
		if !o.draftTypes[nestedName] && !droppedEntries[nestedName] {
			nested = append(nested, o.projectMessage(name, n))
		}
	}
	m.Field, m.OneofDecl, m.NestedType = fields, oneofs, nested
	return m
}

// normalizeForOracle drops what the oracle does not compare: source info, which holds
// positions and comments, and the import lists, which checkAgainstOracle checks separately.
func normalizeForOracle(fd *descriptorpb.FileDescriptorProto) *descriptorpb.FileDescriptorProto {
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

func oracleStablePath(path string) string {
	if rest, ok := strings.CutPrefix(path, apiNextPrefix); ok {
		return stableAPIPrefix + rest
	}
	return path
}

// protocSet compiles files with protoc and returns the descriptors and protoc's output.
func protocSet(t *testing.T, includes, files []string) (*descriptorpb.FileDescriptorSet, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "set.binpb")
	args := []string{"--include_source_info", "--descriptor_set_out=" + out}
	for _, include := range includes {
		args = append(args, "-I"+include)
	}
	output, err := exec.Command("protoc", append(args, files...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("protoc: %v\n%s", err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(data, set); err != nil {
		t.Fatal(err)
	}
	return set, string(output)
}

// protoFiles lists the .proto files under root/prefix, relative to root.
func protoFiles(t *testing.T, root, prefix string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(filepath.Join(root, prefix), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".proto" {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}
