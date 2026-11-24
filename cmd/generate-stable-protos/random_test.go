package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temporalio/api/cmd/generate-stable-protos/internal/oracle"
	"pgregory.net/rapid"
)

// TestRandomSchemas generates random api_next trees with all draft placements and
// requires the generator to accept each one, its output to pass the oracle, and a
// second pass to reproduce it. Comments only sit where the generator accepts them:
// directly above a declaration or on the same line as its opening or closing token.
func TestRandomSchemas(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		root := t.TempDir()
		writeRandomSchema(rt, root, newSchemaGen(rt).files())
		out := t.TempDir()
		if err := testConfig.stableProtosGenerator(t.Context(), root, out); err != nil {
			rt.Fatalf("generator rejected the schema: %v", err)
		}
		result, err := oracle.Check(root, out)
		if err != nil {
			rt.Fatal(err)
		}
		if problems := result.AllProblems(); len(problems) > 0 {
			rt.Fatalf("oracle problems:\n%s", strings.Join(problems, "\n"))
		}
		checkIdempotent(t.Context(), rt, root, out, t.TempDir())
	})
}

func TestRandomSchemasRejectMissingFieldDraft(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		g := newSchemaGen(rt)
		g.startFile("base")
		typ := schemaType{name: "Preview", feature: g.feature()}
		if rapid.Bool().Draw(rt, "enum target") {
			g.enum(typ, "", "")
		} else {
			g.message(typ, "", 0, "")
		}
		format := rapid.SampledFrom(fieldFormats[:]).Draw(rt, "field format")
		fmt.Fprintf(g.b, "message Stable { %s preview = 1; }\n", fmt.Sprintf(format, typ.name))

		root := t.TempDir()
		writeRandomSchema(rt, root, map[string]string{testConfig.nextDir + "/base.proto": g.b.String()})
		err := testConfig.stableProtosGenerator(t.Context(), root, t.TempDir())
		want := fmt.Sprintf("fixture.Stable.preview: field references draft type fixture.Preview and requires draft = %q", typ.feature)
		if err == nil || !strings.Contains(err.Error(), want) {
			rt.Fatalf("expected generator error containing %q, got %v", want, err)
		}
	})
}

func writeRandomSchema(rt *rapid.T, root string, files map[string]string) {
	files[testConfig.draftSchema] = randomDraftSchema
	for _, path := range sortedKeys(files, nil) {
		rt.Logf("%s:\n%s", path, files[path])
		writeFile(rt, filepath.Join(root, path), files[path])
	}
}

const randomDraftSchema = `syntax = "proto3";
package temporal.api.protometa.v1;
import "google/protobuf/descriptor.proto";
extend google.protobuf.FieldOptions { string draft = 98111; }
extend google.protobuf.MessageOptions { string draft_message = 98008; }
extend google.protobuf.EnumOptions { string draft_enum = 98008; }
extend google.protobuf.EnumValueOptions { string draft_enum_value = 98008; }
extend google.protobuf.OneofOptions { string draft_oneof = 98009; }
extend google.protobuf.ServiceOptions { string draft_service = 98008; }
extend google.protobuf.MethodOptions { string draft_method = 98008; }
`

// schemaGen writes a base file and a dependent file, so that references, and the
// imports they need, can cross files.
type schemaGen struct {
	rt       *rapid.T
	b        *strings.Builder
	comments int
	// types are the messages and enums fields in the current file may reference and their draft status.
	types []schemaType
}

type schemaType struct {
	name    string
	feature string
}

func newSchemaGen(rt *rapid.T) *schemaGen { return &schemaGen{rt: rt} }

func (g *schemaGen) files() map[string]string {
	base := g.file("base", rapid.IntRange(1, 4).Draw(g.rt, "base messages"), nil)
	dependent := g.file("dependent", rapid.IntRange(0, 3).Draw(g.rt, "dependent messages"), g.types)
	return map[string]string{
		testConfig.nextDir + "/base.proto":      base,
		testConfig.nextDir + "/dependent.proto": dependent,
	}
}

func (g *schemaGen) startFile(name string) {
	g.b = &strings.Builder{}
	g.b.WriteString("syntax = \"proto3\";\npackage fixture;\n")
	g.b.WriteString("import \"" + testConfig.draftSchema + "\";\n")
	if name == "dependent" {
		g.b.WriteString("import \"" + testConfig.nextDir + "/base.proto\";\n")
	}
}

func (g *schemaGen) file(name string, messages int, imported []schemaType) string {
	g.startFile(name)
	prefix := strings.ToUpper(name[:1])
	enums := make([]schemaType, rapid.IntRange(0, 2).Draw(g.rt, "enums"))
	for i := range enums {
		enums[i] = schemaType{fmt.Sprintf("%sEnum%d", prefix, i), g.maybeFeature("enum")}
	}
	types := make([]schemaType, messages)
	for i := range types {
		types[i] = schemaType{fmt.Sprintf("%s%d", prefix, i), g.maybeFeature("message")}
	}
	g.types = append(append(append([]schemaType{}, imported...), types...), enums...)
	for _, typ := range enums {
		g.enum(typ, "", "")
	}
	for _, typ := range types {
		g.message(typ, "", 0, "")
	}
	for i := range rapid.IntRange(0, 2).Draw(g.rt, "services") {
		g.service(fmt.Sprintf("%sService%d", prefix, i), g.maybeFeature("service"))
	}
	return g.b.String()
}

// comment returns a comment with a word no other comment has, so a leak is visible.
func (g *schemaGen) comment() string {
	g.comments++
	return fmt.Sprintf("// note%d", g.comments)
}

func (g *schemaGen) leading(indent string) {
	if rapid.Bool().Draw(g.rt, "leading comment") {
		g.b.WriteString(indent + g.comment() + "\n")
	}
}

func (g *schemaGen) trailing() string {
	if rapid.Bool().Draw(g.rt, "trailing comment") {
		return " " + g.comment()
	}
	return ""
}

func (g *schemaGen) declaration(indent, text string) {
	g.leading(indent)
	g.line(indent, text)
}

func (g *schemaGen) line(indent, text string) {
	g.b.WriteString(indent + text + g.trailing() + "\n")
}

func (g *schemaGen) feature() string {
	return rapid.SampledFrom([]string{"preview", "feature"}).Draw(g.rt, "feature")
}

func (g *schemaGen) maybeFeature(what string) string {
	if rapid.IntRange(0, 3).Draw(g.rt, "draft "+what) == 0 {
		return g.feature()
	}
	return ""
}

func draftOption(annotation, feature string) string {
	return fmt.Sprintf("(temporal.api.protometa.v1.%s) = %q", annotation, feature)
}

func (g *schemaGen) option(indent, annotation, feature string) {
	g.declaration(indent, "option "+draftOption(annotation, feature)+";")
}

func (g *schemaGen) enum(typ schemaType, indent, inheritedFeature string) {
	g.declaration(indent, "enum "+typ.name+" {")
	inner := indent + "  "
	if inheritedFeature == "" && typ.feature != "" {
		g.option(inner, "draft_enum", typ.feature)
	}
	for i := range rapid.IntRange(1, 4).Draw(g.rt, "enum values") {
		value := fmt.Sprintf("%s_%d = %d", strings.ToUpper(typ.name), i, i)
		if i > 0 && typ.feature == "" {
			if feature := g.maybeFeature("enum value"); feature != "" {
				value += " [" + draftOption("draft_enum_value", feature) + "]"
			}
		}
		g.declaration(inner, value+";")
	}
	g.line(indent, "}")
}

var fieldFormats = [...]string{"%s", "repeated %s", "optional %s", "map<string, %s>"}

func (g *schemaGen) message(typ schemaType, indent string, depth int, inheritedFeature string) {
	g.declaration(indent, "message "+typ.name+" {")
	inner := indent + "  "
	if inheritedFeature == "" && typ.feature != "" {
		g.option(inner, "draft_message", typ.feature)
	}
	number := 0
	field := func(indent, decl, feature string) {
		number++
		text := fmt.Sprintf("%s f%d = %d", decl, number, number)
		if feature != "" {
			text += " [" + draftOption("draft", feature) + "]"
		}
		g.declaration(indent, text+";")
	}
	randomField := func(indent, format, containerFeature string) {
		var ref schemaType
		switch format {
		case "repeated %s", "map<string, %s>":
			ref = g.scalarType()
		default:
			ref = g.fieldType(containerFeature)
		}
		feature := ""
		if containerFeature == "" {
			feature = ref.feature
			if feature == "" {
				feature = g.maybeFeature("field")
			}
		}
		field(indent, fmt.Sprintf(format, ref.name), feature)
	}
	for i := range rapid.IntRange(0, 5).Draw(g.rt, "members") {
		switch kind := rapid.IntRange(0, 7).Draw(g.rt, "member kind"); kind {
		case 0, 1, 2, 3:
			randomField(inner, fieldFormats[kind], typ.feature)
		case 4:
			feature := typ.feature
			if feature == "" {
				feature = g.maybeFeature("oneof")
			}
			g.declaration(inner, fmt.Sprintf("oneof o%d {", i))
			branchIndent := inner + "  "
			if typ.feature == "" && feature != "" {
				g.option(branchIndent, "draft_oneof", feature)
			}
			field(branchIndent, "string", "")
			for range rapid.IntRange(0, 2).Draw(g.rt, "additional branches") {
				randomField(branchIndent, "%s", feature)
			}
			g.line(inner, "}")
		case 5:
			if depth < 2 {
				feature := typ.feature
				if feature == "" {
					feature = g.maybeFeature("message")
				}
				nested := schemaType{fmt.Sprintf("N%d", i), feature}
				g.message(nested, inner, depth+1, typ.feature)
			}
		case 6:
			feature := typ.feature
			if feature == "" {
				feature = g.maybeFeature("enum")
			}
			g.enum(schemaType{fmt.Sprintf("Enum%d", i), feature}, inner, typ.feature)
		default:
			feature := ""
			if typ.feature == "" {
				feature = g.maybeFeature("field")
			}
			field(inner, "string", feature)
		}
	}
	g.line(indent, "}")
}

func (g *schemaGen) fieldType(containerFeature string) schemaType {
	if len(g.types) > 0 && rapid.Bool().Draw(g.rt, "named type") {
		var candidates []schemaType
		for _, typ := range g.types {
			if containerFeature == "" || typ.feature == "" || typ.feature == containerFeature {
				candidates = append(candidates, typ)
			}
		}
		if len(candidates) > 0 {
			return rapid.SampledFrom(candidates).Draw(g.rt, "type")
		}
	}
	return g.scalarType()
}

func (g *schemaGen) scalarType() schemaType {
	return schemaType{name: rapid.SampledFrom([]string{"string", "int32", "bool", "bytes"}).Draw(g.rt, "scalar")}
}

func (g *schemaGen) service(name, feature string) {
	methods := make([]string, rapid.IntRange(1, 3).Draw(g.rt, "RPCs"))
	for i := range methods {
		methodFeature := feature
		if feature == "" && i > 0 {
			methodFeature = g.maybeFeature("method")
		}
		methods[i] = methodFeature
		prefix := fmt.Sprintf("%sCall%d", name, i)
		g.message(schemaType{prefix + "Request", methodFeature}, "", 0, "")
		g.message(schemaType{prefix + "Response", methodFeature}, "", 0, "")
	}
	g.declaration("", "service "+name+" {")
	if feature != "" {
		g.option("  ", "draft_service", feature)
	}
	for i, methodFeature := range methods {
		method := fmt.Sprintf("Call%d", i)
		prefix := name + method
		rpc := fmt.Sprintf("rpc %s(%sRequest) returns (%sResponse)", method, prefix, prefix)
		if feature == "" && methodFeature != "" {
			g.declaration("  ", rpc+" {")
			g.option("    ", "draft_method", methodFeature)
			g.line("  ", "}")
		} else {
			g.declaration("  ", rpc+";")
		}
	}
	g.line("", "}")
}
