$(VERBOSE).SILENT:

# Default target
default: all-install all

ifndef GOPATH
GOPATH := $(shell go env GOPATH)
endif

PROTO_ROOT := .
# List only subdirectories with *.proto files. Sort to remove duplicates.
PROTO_DIRS = $(sort $(dir $(wildcard $(PROTO_ROOT)/temporal/*/*/*.proto)))
PROTO_SERVICES = $(wildcard $(PROTO_ROOT)/temporal/*/service.proto)
PROTO_OUT := .gen
PROTO_IMPORT := $(PROTO_ROOT):$(GOPATH)/src/github.com/temporalio/gogo-protobuf/protobuf

all: grpc

all-install: grpc-install

$(PROTO_OUT):
	mkdir $(PROTO_OUT)

# Compile proto files to go

grpc: gogo-grpc fix-path

go-grpc: clean $(PROTO_OUT)
	echo "Compiling for go-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=$(PROTO_IMPORT) --go_out=plugins=grpc,paths=source_relative:$(PROTO_OUT) $(PROTO_DIR)*.proto;)

gogo-grpc: clean $(PROTO_OUT)
	echo "Compiling for gogo-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=$(PROTO_IMPORT) --gogoslick_out=Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,plugins=grpc,paths=source_relative:$(PROTO_OUT) $(PROTO_DIR)*.proto;)

fix-path:
	mv -f $(PROTO_OUT)/temporal/* $(PROTO_OUT) && rm -d $(PROTO_OUT)/temporal

# Plugins & tools

grpc-install: gogo-protobuf-install
	echo "Installing/updaing gRPC plugins..."
	go get -u google.golang.org/grpc

gogo-protobuf-install: go-protobuf-install
	go get -u github.com/temporalio/gogo-protobuf/protoc-gen-gogoslick

go-protobuf-install:
	go get -u github.com/golang/protobuf/protoc-gen-go

# Clean

clean:
	echo "Deleting generated go files..."
	rm -rf $(PROTO_OUT)
