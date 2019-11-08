.PHONY: grpc yarpc clean yarpc-install grpc-install
$(VERBOSE).SILENT:

SUBDIRS := $(dir $(wildcard */.))

yarpc: yarpc-install clean
	echo "Compiling for YARPC..."
	$(foreach dir,$(SUBDIRS),protoc --proto_path=. --gogoslick_out=paths=source_relative:. ${dir}*.proto;)
	$(foreach dir,$(SUBDIRS),protoc --proto_path=. --yarpc-go_out=. ${dir}*.proto;)

grpc: grpc-install clean
	echo "Compiling for gRPC..."
	$(foreach dir,$(SUBDIRS),protoc --proto_path=. --go_out=plugins=grpc,paths=source_relative:. ${dir}*.proto;)

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
	$(foreach dir,$(SUBDIRS),rm -f ${dir}*.go;)