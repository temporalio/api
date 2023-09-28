package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

type tmplInput struct {
	Type string
}

const header = `
package %s

import "google.golang.org/protobuf/proto"
`

const helperTmpl = `
func (val *{{.Type}}) Marshal() ([]byte, error) {
    return proto.Marshal(val)
}

func (val *{{.Type}}) Unmarshal(buf []byte) error {
    return proto.Unmarshal(buf, val)
}

// Equal returns whether two {{.Type}} values are equivalent by recursively
// comparing the message's fields.
// For more information see the documentation for
// https://pkg.go.dev/google.golang.org/protobuf/proto#Equal
func (this *{{.Type}}) Equal(that interface{}) bool {
    if that == nil {
		return this == nil
	}

    var that1 *{{.Type}}
    switch t := that.(type) {
    case *{{.Type}}:
        that1 = t
    case {{.Type}}:
        that1 = &t
    default:
        return false
    }

    return proto.Equal(this, that1)
}`

// NOTE: If our implementation of Equal is too slow (its reflection-based) it doesn't look too
// hard to generate unrolled versions...
func main() {
	t := template.Must(template.New("helpers").Parse(helperTmpl))

	// Protoc passes pluginpb.CodeGeneratorRequest in via stdin
	// marshalled with Protobuf
	input, _ := io.ReadAll(os.Stdin)
	var req pluginpb.CodeGeneratorRequest
	proto.Unmarshal(input, &req)

	// Initialise our plugin with default options
	opts := protogen.Options{}
	plugin, err := opts.New(&req)
	if err != nil {
		panic(err)
	}

	for _, file := range plugin.Files {
		// Skip protos that aren't ours
		if !strings.Contains(string(file.GoImportPath), "go.temporal.io") {
			continue
		}

		var buf bytes.Buffer
		buf.Write([]byte(fmt.Sprintf(header, file.GoPackageName)))

		for _, msg := range file.Proto.MessageType {
			if err := t.Execute(&buf, tmplInput{Type: *msg.Name}); err != nil {
				panic(err)
			}
		}

		file := plugin.NewGeneratedFile(fmt.Sprintf("%s.go-helpers.go", file.GeneratedFilenamePrefix), ".")
		file.Write(buf.Bytes())
	}

	stdout := plugin.Response()
	out, err := proto.Marshal(stdout)
	if err != nil {
		panic(err)
	}

	fmt.Fprintf(os.Stdout, string(out))
}
