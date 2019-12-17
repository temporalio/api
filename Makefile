.PHONY: grpc yarpc clean yarpc-install grpc-install
$(VERBOSE).SILENT:

# List only subdirectories with *.proto files.
# sort to remove duplicates.
PROTO_DIRS := $(sort $(dir $(wildcard */*.proto)))

PROTO_SERVICES := $(wildcard */service.proto)

yarpc: clean gogo-protobuf
	echo "Compiling for YARPC..."
	$(foreach PROTO_SERVICE,$(PROTO_SERVICES),protoc --proto_path=. --yarpc-go_out=. ${PROTO_SERVICE};)

grpc: clean gogo-grpc

gogo-grpc: clean
	echo "Compiling for gogo-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --gogoslick_out=plugins=grpc,paths=source_relative:. ${PROTO_DIR}*.proto;)

gogo-protobuf: clean
	echo "Compiling for gogo-protobuf..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --gogoslick_out=paths=source_relative:. ${PROTO_DIR}*.proto;)

go-protobuf: clean
	echo "Compiling for go-protobuf..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --go_out=paths=source_relative:. ${PROTO_DIR}*.proto;)

go-grpc: clean
	echo "Compiling for go-gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --go_out=plugins=grpc,paths=source_relative:. ${PROTO_DIR}*.proto;)

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
	$(foreach PROTO_DIR,$(PROTO_DIRS),rm -f ${PROTO_DIR}*.go;)