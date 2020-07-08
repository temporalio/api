$(VERBOSE).SILENT:

# Default target
default: all-install all

ifndef GOPATH
GOPATH := $(shell go env GOPATH)
endif

PROTO_ROOT := .
PROTO_FILES = $(shell find $(PROTO_ROOT) -name "*.proto")
PROTO_DIRS = $(sort $(dir $(PROTO_FILES)))
PROTO_OUT := .gen
PROTO_IMPORT := $(PROTO_ROOT):$(GOPATH)/src/github.com/temporalio/gogo-protobuf/protobuf

all: grpc

all-install: grpc-install api-linter-install buf-install

$(PROTO_OUT):
	mkdir $(PROTO_OUT)

# Compile proto files to go

grpc: buf api-linter gogo-grpc fix-path

go-grpc: clean $(PROTO_OUT)
	echo "Compiling for go-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=$(PROTO_IMPORT) --go_out=plugins=grpc,paths=source_relative:$(PROTO_OUT) $(PROTO_DIR)*.proto;)

gogo-grpc: clean $(PROTO_OUT)
	echo "Compiling for gogo-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=$(PROTO_IMPORT) --gogoslick_out=Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,plugins=grpc,paths=source_relative:$(PROTO_OUT) $(PROTO_DIR)*.proto;)

fix-path:
	mv -f $(PROTO_OUT)/temporal/api/* $(PROTO_OUT) && rm -rf $(PROTO_OUT)/temporal

# Plugins & tools

grpc-install: gogo-protobuf-install
	echo "Installing/updating gRPC plugins..."
	go get -u google.golang.org/grpc

gogo-protobuf-install: go-protobuf-install
	go get -u github.com/temporalio/gogo-protobuf/protoc-gen-gogoslick

go-protobuf-install:
	go get -u github.com/golang/protobuf/protoc-gen-go

api-linter-install:
	echo "Installing/updating api-linter..."
	go get -u github.com/googleapis/api-linter/cmd/api-linter

buf-install:
	echo "Installing/updating buf..."
	go get -u github.com/bufbuild/buf/cmd/buf

# Linters

api-linter:
	echo "Running api-linter..."
	api-linter --set-exit-status --output-format summary --config api-linter.yaml $(PROTO_FILES)

buf:
	echo "Running buf linter..."
	buf check lint

# Clean

clean:
	echo "Deleting generated go files..."
	rm -rf $(PROTO_OUT)
