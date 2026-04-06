package main

import (
	"slices"
	"sort"
	"strings"

	nexusannotationsv1 "github.com/nexus-rpc/nexus-proto-annotations/go/nexusannotations/v1"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"gopkg.in/yaml.v3"
)

func generate(gen *protogen.Plugin) error {
	nexusDoc := newDoc()
	langsDoc := newDoc()

	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		for _, svc := range f.Services {
			for _, m := range svc.Methods {
				opts, ok := m.Desc.Options().(*descriptorpb.MethodOptions)
				if !ok || opts == nil {
					continue
				}
				if !proto.HasExtension(opts, nexusannotationsv1.E_Operation) {
					continue
				}
				opOpts := proto.GetExtension(opts, nexusannotationsv1.E_Operation).(*nexusannotationsv1.OperationOptions)
				if !slices.Contains(opOpts.GetTags(), "exposed") {
					continue
				}

				svcName := string(svc.Desc.Name())
				methodName := string(m.Desc.Name())

				addOperation(nexusDoc, svcName, methodName,
					map[string]string{"$ref": openAPIRef(m.Input.Desc)},
					map[string]string{"$ref": openAPIRef(m.Output.Desc)},
				)

				addOperation(langsDoc, svcName, methodName,
					langRefs(f.Desc, m.Input.Desc),
					langRefs(f.Desc, m.Output.Desc),
				)
			}
		}
	}
	if err := writeFile(gen, "nexus/temporal-json-schema-models-nexusrpc.yaml", nexusDoc); err != nil {
		return err
	}
	return writeFile(gen, "nexus/temporal-proto-models-nexusrpc.yaml", langsDoc)
}

// openAPIRef returns the nexus-rpc-gen multi-file $ref string for a message type,
// referencing the openapiv3.yaml components/schemas entry. The path is relative to nexus/.
//
// Schema key convention used by protoc-gen-openapi (v3):
//
//	{MessageName}  (no package prefix)
//
// e.g. message "SignalWithStartWorkflowExecutionRequest"
// → "../openapi/openapiv3.yaml#/components/schemas/SignalWithStartWorkflowExecutionRequest"
func openAPIRef(msg protoreflect.MessageDescriptor) string {
	return "../openapi/openapiv3.yaml#/components/schemas/" + string(msg.Name())
}

// langRefs builds the map of language-specific type refs for a message.
// Go, Java, dotnet, and Ruby refs are derived from proto file-level package options.
// Python and TypeScript refs are derived from the go_package path, taking only the
// last two path segments ({service}/v{n}).
func langRefs(file protoreflect.FileDescriptor, msg protoreflect.MessageDescriptor) map[string]string {
	opts, ok := file.Options().(*descriptorpb.FileOptions)
	if !ok || opts == nil {
		return nil
	}
	name := string(msg.Name())
	refs := make(map[string]string)

	if pkg := opts.GetGoPackage(); pkg != "" {
		// strip the ";alias" suffix (e.g. "go.temporal.io/api/workflowservice/v1;workflowservice")
		pkg = strings.SplitN(pkg, ";", 2)[0]
		refs["$goRef"] = pkg + "." + name

		segments := strings.Split(pkg, "/")
		if len(segments) >= 2 {
			tail := strings.Join(segments[len(segments)-2:], "/")
			refs["$pythonRef"] = "temporalio.api." + strings.ReplaceAll(tail, "/", ".") + "." + name
			refs["$typescriptRef"] = "@temporalio/api/" + tail + "." + name
		}
	}
	if pkg := opts.GetJavaPackage(); pkg != "" {
		refs["$javaRef"] = pkg + "." + name
	}
	if pkg := opts.GetRubyPackage(); pkg != "" {
		refs["$rubyRef"] = pkg + "::" + name
	}
	if pkg := opts.GetCsharpNamespace(); pkg != "" {
		refs["$dotnetRef"] = pkg + "." + name
	}
	if len(refs) == 0 {
		return nil
	}
	return refs
}

// newDoc creates a yaml.Node document with the "nexusrpc: 1.0.0" header
// and an empty "services" mapping node, returned as a *yaml.Node (document node).
func newDoc() *yaml.Node {
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	doc.Content = []*yaml.Node{root}

	root.Content = append(root.Content,
		scalarNode("nexusrpc"),
		scalarNode("1.0.0"),
		scalarNode("services"),
		&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"},
	)
	return doc
}

// servicesNode returns the "services" mapping node from a doc created by newDoc.
func servicesNode(doc *yaml.Node) *yaml.Node {
	root := doc.Content[0]
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "services" {
			return root.Content[i+1]
		}
	}
	panic("services node not found")
}

// addOperation inserts a service → operation → {input, output} entry into doc.
// Services and operations are inserted in the order first encountered.
func addOperation(doc *yaml.Node, svcName, methodName string, input, output map[string]string) {
	svcs := servicesNode(doc)

	// find or create service node
	var svcOps *yaml.Node
	for i := 0; i < len(svcs.Content)-1; i += 2 {
		if svcs.Content[i].Value == svcName {
			// find "operations" within service mapping
			svcMap := svcs.Content[i+1]
			for j := 0; j < len(svcMap.Content)-1; j += 2 {
				if svcMap.Content[j].Value == "operations" {
					svcOps = svcMap.Content[j+1]
				}
			}
		}
	}
	if svcOps == nil {
		svcMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		svcOps = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		svcMap.Content = append(svcMap.Content,
			scalarNode("operations"),
			svcOps,
		)
		svcs.Content = append(svcs.Content, scalarNode(svcName), svcMap)
	}

	// build operation node
	opNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	if len(input) > 0 {
		opNode.Content = append(opNode.Content, scalarNode("input"), mapNode(input))
	}
	if len(output) > 0 {
		opNode.Content = append(opNode.Content, scalarNode("output"), mapNode(output))
	}
	svcOps.Content = append(svcOps.Content, scalarNode(methodName), opNode)
}

// mapNode serializes a map[string]string as a yaml mapping node with keys in sorted order.
func mapNode(m map[string]string) *yaml.Node {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range keys {
		node.Content = append(node.Content, scalarNode(k), scalarNode(m[k]))
	}
	return node
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func writeFile(gen *protogen.Plugin, name string, doc *yaml.Node) error {
	f := gen.NewGeneratedFile(name, "")
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Close()
}
