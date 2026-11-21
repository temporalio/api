package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestRandomSchemas generates random api_next trees with draft fields and messages and
// requires the generator to accept each one and a
// second pass to reproduce it. Comments only sit where the generator accepts them:
// directly above a declaration or after it on the same line.
func TestRandomSchemas(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		root := t.TempDir()
		writeFile(rt, filepath.Join(root, testConfig.draftSchema), randomDraftSchema)
		for path, text := range newSchemaGen(rt).files() {
			rt.Logf("%s:\n%s", path, text)
			writeFile(rt, filepath.Join(root, path), text)
		}
		out := t.TempDir()
		if err := testConfig.stableProtosGenerator(t.Context(), root, out); err != nil {
			rt.Fatalf("generator rejected the schema: %v", err)
		}
		checkIdempotent(t.Context(), rt, root, out, t.TempDir())
	})
}

const randomDraftSchema = `syntax = "proto3";
package temporal.api.protometa.v1;
import "google/protobuf/descriptor.proto";
extend google.protobuf.FieldOptions { string draft = 98111; }
extend google.protobuf.MessageOptions { string draft_message = 98008; }
`

// schemaGen writes a base file and a dependent file, so that references, and the
// imports they need, can cross files.
type schemaGen struct {
	rt       *rapid.T
	b        *strings.Builder
	comments int
	// types are the messages fields in the current file may reference and their draft status.
	types []schemaType
}

type schemaType struct {
	name  string
	draft bool
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

func (g *schemaGen) file(name string, messages int, imported []schemaType) string {
	g.b = &strings.Builder{}
	g.b.WriteString("syntax = \"proto3\";\npackage fixture;\n")
	g.b.WriteString("import \"" + testConfig.draftSchema + "\";\n")
	if name == "dependent" {
		g.b.WriteString("import \"" + testConfig.nextDir + "/base.proto\";\n")
	}
	types := make([]schemaType, messages)
	for i := range types {
		types[i] = schemaType{fmt.Sprintf("%s%d", strings.ToUpper(name[:1]), i), g.draft("message")}
	}
	g.types = append(append([]schemaType{}, imported...), types...)
	for _, typ := range types {
		g.message(typ, "", 0, false)
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

func (g *schemaGen) draft(what string) bool {
	return rapid.IntRange(0, 3).Draw(g.rt, "draft "+what) == 0
}

func (g *schemaGen) message(typ schemaType, indent string, depth int, inheritedDraft bool) {
	g.leading(indent)
	g.b.WriteString(indent + "message " + typ.name + " {" + g.trailing() + "\n")
	inner := indent + "  "
	if !inheritedDraft && typ.draft {
		g.b.WriteString(inner + `option (temporal.api.protometa.v1.draft_message) = "feature";` + "\n")
	}
	number := 0
	field := func(decl string, annotate bool) {
		number++
		g.leading(inner)
		g.b.WriteString(fmt.Sprintf("%s%s f%d = %d", inner, decl, number, number))
		if annotate {
			g.b.WriteString(` [(temporal.api.protometa.v1.draft) = "feature"]`)
		}
		g.b.WriteString(";" + g.trailing() + "\n")
	}
	randomField := func(prefix, suffix string) {
		ref := g.fieldType()
		field(prefix+ref.name+suffix, !typ.draft && (ref.draft || g.draft("field")))
	}
	for i := range rapid.IntRange(0, 5).Draw(g.rt, "members") {
		switch rapid.IntRange(0, 6).Draw(g.rt, "member kind") {
		case 0:
			randomField("", "")
		case 1:
			randomField("repeated ", "")
		case 2:
			randomField("optional ", "")
		case 3:
			randomField("map<string, ", ">")
		case 4:
			g.leading(inner)
			g.b.WriteString(fmt.Sprintf("%soneof o%d {%s\n", inner, i, g.trailing()))
			saved := inner
			inner += "  "
			field("string", false)
			for range rapid.IntRange(0, 2).Draw(g.rt, "additional branches") {
				randomField("", "")
			}
			inner = saved
			g.b.WriteString(inner + "}" + g.trailing() + "\n")
		case 5:
			if depth < 2 {
				nested := schemaType{fmt.Sprintf("N%d", i), typ.draft || g.draft("message")}
				g.message(nested, inner, depth+1, typ.draft)
			}
		default:
			field("string", !typ.draft && g.draft("field"))
		}
	}
	g.b.WriteString(indent + "}" + g.trailing() + "\n")
}

func (g *schemaGen) fieldType() schemaType {
	if len(g.types) > 0 && rapid.Bool().Draw(g.rt, "message type") {
		return rapid.SampledFrom(g.types).Draw(g.rt, "type")
	}
	return schemaType{name: rapid.SampledFrom([]string{"string", "int32", "bool", "bytes"}).Draw(g.rt, "scalar")}
}
