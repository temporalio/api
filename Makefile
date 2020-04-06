$(VERBOSE).SILENT:

# default target
default: all-install all

ifndef GOPATH
GOPATH := $(shell go env GOPATH)
endif

PROTO_ROOT := .
# List only subdirectories with *.proto files. Sort to remove duplicates.
PROTO_DIRS = $(sort $(dir $(wildcard $(PROTO_ROOT)/*/*.proto)))
PROTO_SERVICES = $(wildcard $(PROTO_ROOT)/*/service.proto)
PROTO_OUT := .gen
PROTO_IMPORT := $(PROTO_ROOT):$(GOPATH)/src/github.com/gogo/protobuf/protobuf

all: grpc

all-install: grpc-install

$(PROTO_OUT):
	mkdir $(PROTO_OUT)

# Compile proto files to go

grpc: go-grpc gogo-grpc

go-grpc: clean $(PROTO_OUT)
	echo "Compiling for go-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=$(PROTO_IMPORT) --go_out=plugins=grpc,paths=source_relative:$(PROTO_OUT) $(PROTO_DIR)*.proto;)

gogo-grpc: clean $(PROTO_OUT)
	echo "Compiling for gogo-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=$(PROTO_IMPORT) --gogofaster_out=Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,plugins=grpc,paths=source_relative:$(PROTO_OUT) $(PROTO_DIR)*.proto;)

# Plugins & tools

grpc-install: gogo-protobuf-install
	echo "Installing/updaing gRPC plugins..."
	go get -u google.golang.org/grpc

gogo-protobuf-install: go-protobuf-install
	go get -u github.com/gogo/protobuf/protoc-gen-gogofaster

go-protobuf-install:
	go get -u github.com/golang/protobuf/protoc-gen-go

# clean

clean:
	echo "Deleting generated go files..."
	rm -rf $(PROTO_OUT)
