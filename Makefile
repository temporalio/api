.PHONY: grpc yarpc clean yarpc-install grpc-install
$(VERBOSE).SILENT:

# default target
default: yarpc

# List only subdirectories with *.proto files.
# sort to remove duplicates.
PROTO_DIRS := $(sort $(dir $(wildcard */*.proto)))
PROTO_SERVICES := $(wildcard */service.proto)
GEN_DIR = .gen/proto

yarpc: gogo-protobuf
	echo "Compiling for YARPC..."
	$(foreach PROTO_SERVICE,$(PROTO_SERVICES),protoc --proto_path=. --yarpc-go_out=$(GEN_DIR) $(PROTO_SERVICE);)

grpc: gogo-grpc

gogo-grpc: clean $(GEN_DIR)
	echo "Compiling for gogo-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --gogoslick_out=plugins=grpc,paths=source_relative:$(GEN_DIR) $(PROTO_DIR)*.proto;)

gogo-protobuf: clean $(GEN_DIR)
	echo "Compiling for gogo-protobuf..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --gogoslick_out=paths=source_relative:$(GEN_DIR) $(PROTO_DIR)*.proto;)

go-protobuf: clean $(GEN_DIR)
	echo "Compiling for go-protobuf..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --go_out=paths=source_relative:$(GEN_DIR) $(PROTO_DIR)*.proto;)

go-grpc: clean $(GEN_DIR)
	echo "Compiling for go-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --go_out=plugins=grpc,paths=source_relative:$(GEN_DIR) $(PROTO_DIR)*.proto;)

$(GEN_DIR):
	mkdir -p $(GEN_DIR)

yarpc-install: gogo-protobuf-install
	echo "Installing/updaing YARPC plugins..."
	go get -u go.uber.org/yarpc/encoding/protobuf/protoc-gen-yarpc-go

grpc-install: gogo-protobuf-install
	echo "Installing/updaing gRPC plugins..."
	go get -u google.golang.org/grpc

gogo-protobuf-install: go-protobuf-install
	go get -u github.com/gogo/protobuf/protoc-gen-gogoslick

go-protobuf-install:
	go get -u github.com/golang/protobuf/protoc-gen-go

clean:
	echo "Deleting generated go files..."
	rm -rf .gen/proto