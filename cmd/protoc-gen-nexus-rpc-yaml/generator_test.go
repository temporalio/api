package main

import (
	"context"
	"os"
	"testing"

	"github.com/bufbuild/protocompile"
	nexusannotationsv1 "github.com/nexus-rpc/nexus-proto-annotations/go/nexusannotations/v1"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// runGenerate compiles testdata/test/v1/service.proto using the given plugin
// parameter string and returns the generated file contents keyed by filename.
//
// Known dependencies (google/protobuf/descriptor.proto and
// nexusannotations/v1/options.proto) are supplied as pre-compiled Go
// descriptors so that extension values are deserialized into Go-generated
// types rather than *dynamicpb.Message.
func runGenerate(t *testing.T, parameter string) map[string]string {
	t.Helper()

	resolver := protocompile.CompositeResolver{
		// Return pre-compiled Go file descriptors for known dependencies.
		// This ensures that the nexus extension is deserialised into the
		// Go-generated *nexusannotationsv1.OperationOptions type, not a
		// dynamic protobuf message, which would cause a type-assertion panic
		// in shouldIncludeOperation.
		protocompile.ResolverFunc(func(path string) (protocompile.SearchResult, error) {
			switch path {
			case "google/protobuf/descriptor.proto":
				return protocompile.SearchResult{Desc: descriptorpb.File_google_protobuf_descriptor_proto}, nil
			case "nexusannotations/v1/options.proto":
				return protocompile.SearchResult{Desc: nexusannotationsv1.File_nexusannotations_v1_options_proto}, nil
			}
			return protocompile.SearchResult{}, protoregistry.NotFound
		}),
		// Fall back to reading source files from the testdata directory.
		&protocompile.SourceResolver{ImportPaths: []string{"testdata"}},
	}

	compiledFiles, err := (&protocompile.Compiler{Resolver: resolver}).Compile(
		context.Background(), "test/v1/service.proto",
	)
	if err != nil {
		t.Fatalf("compiling test proto: %v", err)
	}

	// Collect all file descriptor protos in topological order (imports before importers).
	// Each FileDescriptorProto is round-tripped through proto.Marshal + proto.Unmarshal
	// to normalize extension fields. protocompile stores extension values as
	// *dynamicpb.Message objects; the round-trip serializes them to raw bytes and lets
	// proto.Unmarshal deserialize them as the registered Go-generated types (e.g.
	// *nexusannotationsv1.OperationOptions). Without this, proto.GetExtension in the
	// generator would panic with "invalid type: got *dynamicpb.Message".
	seen := make(map[string]bool)
	var protoFiles []*descriptorpb.FileDescriptorProto
	var collect func(f protoreflect.FileDescriptor)
	collect = func(f protoreflect.FileDescriptor) {
		if seen[f.Path()] {
			return
		}
		seen[f.Path()] = true
		for i := 0; i < f.Imports().Len(); i++ {
			collect(f.Imports().Get(i))
		}
		raw, err := proto.Marshal(protodesc.ToFileDescriptorProto(f))
		if err != nil {
			t.Fatalf("marshaling file descriptor %s: %v", f.Path(), err)
		}
		var fdp descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(raw, &fdp); err != nil {
			t.Fatalf("unmarshaling file descriptor %s: %v", f.Path(), err)
		}
		protoFiles = append(protoFiles, &fdp)
	}
	for _, f := range compiledFiles {
		collect(f)
	}

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"test/v1/service.proto"},
		ProtoFile:      protoFiles,
		Parameter:      proto.String(parameter),
	}
	gen, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("creating plugin: %v", err)
	}
	if err := generate(gen); err != nil {
		t.Fatalf("generate: %v", err)
	}

	result := make(map[string]string)
	for _, f := range gen.Response().File {
		result[f.GetName()] = f.GetContent()
	}
	return result
}

// compareGolden compares got against the contents of goldenPath.
// Set UPDATE_GOLDEN=1 to overwrite the golden file with the current output
// instead of comparing, making it easy to refresh after intentional generator changes.
func compareGolden(t *testing.T, got, goldenPath string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("updating golden file %s: %v", goldenPath, err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("output mismatch for %s:\ngot:\n%s\nwant:\n%s", goldenPath, got, string(want))
	}
}

func TestGenerate_ExposedOperation(t *testing.T) {
	const params = "openapi_ref_prefix=../openapi/openapiv3.yaml#/components/schemas/," +
		"nexusrpc_out=nexus/nexusrpc.yaml," +
		"nexusrpc_langs_out=nexus/nexusrpc.langs.yaml," +
		"python_package_prefix=example.api," +
		"typescript_package_prefix=@example/api," +
		"include_operation_tags=exposed"

	files := runGenerate(t, params)

	compareGolden(t, files["nexus/nexusrpc.yaml"], "testdata/nexusrpc.yaml")
	compareGolden(t, files["nexus/nexusrpc.langs.yaml"], "testdata/nexusrpc_langs.yaml")
}

func TestGenerate_ExcludeTag_ProducesNoFiles(t *testing.T) {
	const params = "nexusrpc_out=nexus/nexusrpc.yaml," +
		"nexusrpc_langs_out=nexus/nexusrpc.langs.yaml," +
		"exclude_operation_tags=exposed"

	files := runGenerate(t, params)

	if len(files) != 0 {
		t.Errorf("expected no files when operation tag is excluded, got: %v", files)
	}
}

func TestGenerate_NoIncludeFilter_IncludesAnnotatedOperations(t *testing.T) {
	// When no include_operation_tags is set, any annotated operation is included.
	const params = "nexusrpc_langs_out=nexus/nexusrpc.langs.yaml"

	files := runGenerate(t, params)

	if _, ok := files["nexus/nexusrpc.langs.yaml"]; !ok {
		t.Error("expected nexusrpc.langs.yaml to be generated when no include filter is set")
	}
}
