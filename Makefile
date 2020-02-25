.PHONY: grpc clean grpc-install
$(VERBOSE).SILENT:

# default target
default: all-install all

# List only subdirectories with *.proto files.
# sort to remove duplicates.
PROTO_ROOT := .
PROTO_DIRS = $(sort $(dir $(wildcard $(PROTO_ROOT)/*/*.proto)))
PROTO_SERVICES = $(wildcard $(PROTO_ROOT)/*/service.proto)
PROTO_OUT := .gen
PROTO_IMPORT := $(PROTO_ROOT)

all: go-grpc

all-install: grpc-install

$(PROTO_OUT):
	mkdir $(PROTO_OUT)

# Compile proto files to go

go-grpc: clean $(PROTO_OUT)
	echo "Compiling for go-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=$(PROTO_IMPORT) --go_out=plugins=grpc,paths=source_relative:$(PROTO_OUT) $(PROTO_DIR)*.proto;)

# Plugins & tools

grpc-install: gogo-protobuf-install
	echo "Installing/updaing gRPC plugins..."
	go get -u google.golang.org/grpc

gogo-protobuf-install: go-protobuf-install
	go get -u github.com/gogo/protobuf/protoc-gen-gogoslick

go-protobuf-install:
	go get -u github.com/golang/protobuf/protoc-gen-go

# clean

clean:
	echo "Deleting generated go files..."
	rm -rf $(PROTO_OUT)