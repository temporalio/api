.PHONY: grpc yarpc clean yarpc-install grpc-install
$(VERBOSE).SILENT:

# List only subdirectories with *.proto files.
# sort to remove duplicates.
PROTO_DIRS := $(sort $(dir $(wildcard */*.proto)))

yarpc: clean
	echo "Compiling for YARPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --gogoslick_out=paths=source_relative:. ${PROTO_DIR}*.proto;)
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --yarpc-go_out=. ${PROTO_DIR}*.proto;)

grpc: clean
	echo "Compiling for gRPC..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),protoc --proto_path=. --go_out=plugins=grpc,paths=source_relative:. ${PROTO_DIR}*.proto;)

yarpc-install:
	echo "Installing/updaing YARPC plugin..."
	go get -u github.com/gogo/protobuf/protoc-gen-gogoslick
	go get -u go.uber.org/yarpc/encoding/protobuf/protoc-gen-yarpc-go

grpc-install:
	echo "Installing/updaing gRPC plugin..."
	go get -u github.com/golang/protobuf/protoc-gen-go
	go get -u google.golang.org/grpc

clean:
	echo "Deleting old files..."
	$(foreach PROTO_DIR,$(PROTO_DIRS),rm -f ${PROTO_DIR}*.go;)