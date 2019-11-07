.PHONY: grpc yarpc clean yarpc-install grpc-install

SUBDIRS := $(dir $(wildcard */.))

yarpc-install:
	go get -u github.com/gogo/protobuf/protoc-gen-gogoslick
	go get -u go.uber.org/yarpc/encoding/protobuf/protoc-gen-yarpc-go

grpc-install:
	go get -u github.com/golang/protobuf/protoc-gen-go
	go get -u google.golang.org/grpc

clean:
	$(foreach dir,$(SUBDIRS),rm -f ${dir}*.go;)

yarpc: yarpc-install clean
	$(foreach dir,$(SUBDIRS),protoc --proto_path=. --gogoslick_out=paths=source_relative:. ${dir}*.proto;)
	$(foreach dir,$(SUBDIRS),protoc --proto_path=. --yarpc-go_out=. ${dir}*.proto;)

grpc: grpc-install clean
	$(foreach dir,$(SUBDIRS),protoc --proto_path=. --go_out=plugins=grpc,paths=source_relative:. ${dir}*.proto)