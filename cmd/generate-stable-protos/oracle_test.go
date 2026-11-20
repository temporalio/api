package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/descriptorpb"
)

// checkAgainstOracle compares the generator's output in out against an independent
// reference: protoc compiles the api_next input and the stable output, and the stable
// descriptors must equal the input descriptors with the draft declarations projected
// out. The projection works on descriptors, where removing a declaration is simple, so
// it shares no logic with the generator's AST editing.
func checkAgainstOracle(t *testing.T, root, out string) {
	t.Helper()
	inputTexts := readProtoTree(t, root, apiNextPrefix)
	outputTexts := readProtoTree(t, out, stableAPIPrefix)
	input, _ := protocSet(t, []string{root}, slices.Sorted(maps.Keys(inputTexts)))
	// The output directory comes first so it shadows any stable tree checked in under root.
	output, warnings := protocSet(t, []string{out, root}, slices.Sorted(maps.Keys(outputTexts)))
	if strings.Contains(warnings, "is unused") {
		t.Fatalf("stable output has unused imports:\n%s", warnings)
	}

	oracle := newDraftOracle(t, input)
	expected := map[string]*descriptorpb.FileDescriptorProto{}
	removed := map[string][][]int32{}
	inputDeps := map[string]bool{}
	for _, fd := range input.GetFile() {
		if fd.GetName() == draftTagsPath {
			continue
		}
		projected, paths := oracle.projectFile(fd)
		expected[projected.GetName()] = projected
		removed[fd.GetName()] = paths
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
	checkComments(t, inputTexts, outputTexts, input, output, removed)
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

// projectFile returns fd without its draft declarations, and the source paths of the
// outermost declarations it removed.
func (o *draftOracle) projectFile(fd *descriptorpb.FileDescriptorProto) (*descriptorpb.FileDescriptorProto, [][]int32) {
	fd = proto.Clone(fd).(*descriptorpb.FileDescriptorProto)
	fd.Name = new(oracleStablePath(fd.GetName()))
	prefix := packagePrefix(fd)
	var msgs []*descriptorpb.DescriptorProto
	var removed [][]int32
	for i, m := range fd.GetMessageType() {
		path := []int32{fileMessagesTag, int32(i)}
		if o.draftTypes[prefix+"."+m.GetName()] {
			removed = append(removed, path)
			continue
		}
		msgs = append(msgs, o.projectMessage(prefix, m, path, &removed))
	}
	fd.MessageType = msgs
	return normalizeForOracle(fd), removed
}

// Source paths, per the field numbers in descriptor.proto.
const (
	fileDependenciesTag = 3
	fileMessagesTag     = 4
	messageFieldsTag    = 2
	messageNestedTag    = 3
	messageOneofsTag    = 8
)

func (o *draftOracle) projectMessage(prefix string, m *descriptorpb.DescriptorProto, path []int32, removed *[][]int32) *descriptorpb.DescriptorProto {
	name := prefix + "." + m.GetName()
	var fields []*descriptorpb.FieldDescriptorProto
	var draftFields []int
	droppedEntries := map[string]bool{}
	kept := map[int32]int{}
	for i, f := range m.GetField() {
		if o.isDraftField(f) {
			if o.mapEntries[f.GetTypeName()] != nil {
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
		case o.draftTypes[nestedName]:
			*removed = append(*removed, nestedPath)
		case droppedEntries[nestedName]:
			// A map entry is synthetic and has no source; its field is recorded above.
		default:
			nested = append(nested, o.projectMessage(name, n, nestedPath, removed))
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

// readProtoTree reads the files under root/prefix, keyed by their path relative to root.
func readProtoTree(t *testing.T, root, prefix string) map[string]string {
	t.Helper()
	files := map[string]string{}
	for path, text := range readTree(t, filepath.Join(root, prefix)) {
		files[prefix+path] = text
	}
	return files
}

// checkComments requires every input comment to appear in the output, except the
// comments inside or attached to a removed declaration or a pruned import, and no other
// comment to appear. Comments are scanned from the source text rather than taken from
// SourceCodeInfo, which has no place for a comment inside an option value.
func checkComments(t *testing.T, inputTexts, outputTexts map[string]string, input, output *descriptorpb.FileDescriptorSet, removed map[string][][]int32) {
	t.Helper()
	outputDeps := map[string][]string{}
	for _, fd := range output.GetFile() {
		outputDeps[fd.GetName()] = fd.GetDependency()
	}
	for _, fd := range input.GetFile() {
		if fd.GetName() == draftTagsPath {
			continue
		}
		stable := oracleStablePath(fd.GetName())
		locations := map[string]*descriptorpb.SourceCodeInfo_Location{}
		for _, loc := range fd.GetSourceCodeInfo().GetLocation() {
			if key := fmt.Sprint(loc.GetPath()); locations[key] == nil {
				locations[key] = loc
			}
		}
		gone := removed[fd.GetName()]
		for i, dep := range fd.GetDependency() {
			if !slices.Contains(outputDeps[stable], oracleStablePath(dep)) {
				gone = append(gone, []int32{fileDependenciesTag, int32(i)})
			}
		}

		var spans [][]int32
		// surplus counts each comment line in the output minus its count in the input.
		surplus := map[string]int{}
		for _, path := range gone {
			loc := locations[fmt.Sprint(path)]
			if loc == nil {
				t.Fatalf("%s: no source location for removed path %v", fd.GetName(), path)
			}
			spans = append(spans, loc.GetSpan())
			// Leading comments sit before the span, so the scan below counts them. Trailing
			// comments are left to the scan: one on the span's last line counts as part of
			// the declaration, even after a closing brace, where protoc attaches nothing.
			for _, text := range append(slices.Clone(loc.GetLeadingDetachedComments()), loc.GetLeadingComments()) {
				for _, line := range normalizeComment(text) {
					surplus[line]++
				}
			}
		}
		for _, c := range scanComments(inputTexts[fd.GetName()]) {
			if !slices.ContainsFunc(spans, c.within) {
				for _, line := range commentLines([]comment{c}) {
					surplus[line]--
				}
			}
		}

		text, ok := strings.CutPrefix(outputTexts[stable], generatedHeader)
		if !ok {
			t.Fatalf("%s: missing generated header", stable)
		}
		for _, line := range commentLines(scanComments(text)) {
			surplus[line]++
		}
		var diffs []string
		for line, n := range surplus {
			if n != 0 {
				diffs = append(diffs, fmt.Sprintf("%+d %q", n, line))
			}
		}
		if len(diffs) > 0 {
			slices.Sort(diffs)
			t.Errorf("%s: comment lines differ from the input less removed declarations (+leaked, -lost):\n%s", stable, strings.Join(diffs, "\n"))
		}
	}
}

type comment struct {
	line, col int // zero-based, columns counted like protoc: tabs advance to a multiple of 8
	text      string
}

// within reports whether c starts inside a SourceCodeInfo span or later on its last line.
func (c comment) within(span []int32) bool {
	startLine, startCol, endLine, endCol := span[0], span[1], span[0], span[2]
	if len(span) == 4 {
		endLine, endCol = span[2], span[3]
	}
	pos := []int32{int32(c.line), int32(c.col)}
	return slices.Compare(pos, []int32{startLine, startCol}) >= 0 && (slices.Compare(pos, []int32{endLine, endCol}) < 0 || pos[0] == endLine)
}

// scanComments returns the comments in proto source, skipping string literals.
func scanComments(src string) []comment {
	var comments []comment
	line, col := 0, 0
	advance := func(n int, i int) int {
		for _, b := range []byte(src[i : i+n]) {
			switch b {
			case '\n':
				line, col = line+1, 0
			case '\t':
				col += 8 - col%8
			default:
				col++
			}
		}
		return i + n
	}
	for i := 0; i < len(src); {
		rest := src[i:]
		switch {
		case rest[0] == '"' || rest[0] == '\'':
			n := 1
			for n < len(rest) && rest[n] != rest[0] && rest[n] != '\n' {
				if rest[n] == '\\' {
					n++
				}
				n++
			}
			i = advance(min(n+1, len(rest)), i)
		case strings.HasPrefix(rest, "//"), strings.HasPrefix(rest, "/*"):
			end := len(rest)
			if rest[1] == '/' {
				if n := strings.IndexByte(rest, '\n'); n >= 0 {
					end = n
				}
			} else if n := strings.Index(rest[2:], "*/"); n >= 0 {
				end = n + 4
			}
			comments = append(comments, comment{line, col, rest[:end]})
			i = advance(end, i)
		default:
			i = advance(1, i)
		}
	}
	return comments
}

// commentLines strips the comment delimiters and normalizes each line.
func commentLines(comments []comment) []string {
	var lines []string
	for _, c := range comments {
		text, block := strings.CutPrefix(c.text, "/*")
		if block {
			text = strings.TrimSuffix(text, "*/")
		} else {
			text = strings.TrimPrefix(text, "//")
		}
		lines = append(lines, normalizeComment(text)...)
	}
	return lines
}

// normalizeComment splits comment text into trimmed, nonempty lines without the leading
// "*" of block comment margins, matching both raw comments and protoc's stripped ones.
func normalizeComment(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
