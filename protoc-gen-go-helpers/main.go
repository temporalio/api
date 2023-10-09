// The MIT License
//
// Copyright (c) 2023 Temporal Technologies Inc.  All rights reserved.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package main

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
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

func (val *{{.Type}}) Size() int {
    return proto.Size(val)
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
	opts := protogen.Options{}
	opts.Run(func(plugin *protogen.Plugin) error {
		t, err := template.New("helpers").Parse(helperTmpl)
		if err != nil {
			return err
		}

		for _, file := range plugin.Files {
			// Skip protos that aren't ours
			if !file.Generate || !strings.Contains(string(file.GoImportPath), "go.temporal.io") || len(file.Proto.MessageType) == 0 {
				continue
			}

			var buf bytes.Buffer
			buf.Write([]byte(fmt.Sprintf(header, file.GoPackageName)))

			for _, msg := range file.Proto.MessageType {
				if err := t.Execute(&buf, tmplInput{Type: *msg.Name}); err != nil {
					return fmt.Errorf("failed to execute template on type %s: %s", *msg.Name, err)
				}
			}

			fmtd, err := format.Source(buf.Bytes())
			if err != nil {
				return fmt.Errorf("failed to format generated source: %w", err)
			}

			gf := plugin.NewGeneratedFile(fmt.Sprintf("%s.go-helpers.go", file.GeneratedFilenamePrefix), ".")
			gf.Write(fmtd)
		}

		return nil
	})
}
