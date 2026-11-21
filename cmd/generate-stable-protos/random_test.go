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
	// types are the message names fields in the current file may reference.
	types []string
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

func (g *schemaGen) file(name string, messages int, imported []string) string {
	g.b = &strings.Builder{}
	g.b.WriteString("syntax = \"proto3\";\npackage fixture;\n")
	g.b.WriteString("import \"" + testConfig.draftSchema + "\";\n")
	if name == "dependent" {
		g.b.WriteString("import \"" + testConfig.nextDir + "/base.proto\";\n")
	}
	names := make([]string, messages)
	for i := range names {
		names[i] = fmt.Sprintf("%s%d", strings.ToUpper(name[:1]), i)
	}
	g.types = append(append([]string{}, imported...), names...)
	for _, n := range names {
		g.message(n, "", 0, false)
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

func (g *schemaGen) message(name, indent string, depth int, inheritedDraft bool) {
	g.leading(indent)
	g.b.WriteString(indent + "message " + name + " {" + g.trailing() + "\n")
	inner := indent + "  "
	draft := inheritedDraft
	if !inheritedDraft && g.draft("message") {
		g.b.WriteString(inner + `option (temporal.api.protometa.v1.draft_message) = "feature";` + "\n")
		draft = true
	}
	number := 0
	field := func(typ string, canAnnotate bool) {
		number++
		g.leading(inner)
		g.b.WriteString(fmt.Sprintf("%s%s f%d = %d", inner, typ, number, number))
		if canAnnotate && !draft && g.draft("field") {
			g.b.WriteString(` [(temporal.api.protometa.v1.draft) = "feature"]`)
		}
		g.b.WriteString(";" + g.trailing() + "\n")
	}
	for i := range rapid.IntRange(0, 5).Draw(g.rt, "members") {
		switch rapid.IntRange(0, 6).Draw(g.rt, "member kind") {
		case 0:
			field(g.fieldType(), true)
		case 1:
			field("repeated "+g.fieldType(), true)
		case 2:
			field("optional "+g.fieldType(), true)
		case 3:
			field("map<string, "+g.fieldType()+">", true)
		case 4:
			g.leading(inner)
			g.b.WriteString(fmt.Sprintf("%soneof o%d {%s\n", inner, i, g.trailing()))
			saved := inner
			inner += "  "
			field("string", false)
			for range rapid.IntRange(0, 2).Draw(g.rt, "additional branches") {
				field(g.fieldType(), true)
			}
			inner = saved
			g.b.WriteString(inner + "}" + g.trailing() + "\n")
		case 5:
			if depth < 2 {
				g.message(fmt.Sprintf("N%d", i), inner, depth+1, draft)
			}
		default:
			field("string", true)
		}
	}
	g.b.WriteString(indent + "}" + g.trailing() + "\n")
}

func (g *schemaGen) fieldType() string {
	if len(g.types) > 0 && rapid.Bool().Draw(g.rt, "message type") {
		return rapid.SampledFrom(g.types).Draw(g.rt, "type")
	}
	return rapid.SampledFrom([]string{"string", "int32", "bool", "bytes"}).Draw(g.rt, "scalar")
}
