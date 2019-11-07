.PHONY: grpc yarpc clean yarpc-install grpc-install

yarpc-install:
	go get -u github.com/gogo/protobuf/protoc-gen-gogoslick
	go get -u go.uber.org/yarpc/encoding/protobuf/protoc-gen-yarpc-go

grpc-install:
	go get -u github.com/golang/protobuf/protoc-gen-go
	go get -u google.golang.org/grpc

clean:
	for dir in ./*/
	do
		rm -f ${dir}*.go
	done

yarpc: yarpc-install clean
	for dir in ./*/
	do
		protoc --proto_path=proto --gogoslick_out=paths=source_relative:proto ${dir}*.proto
		protoc --proto_path=proto --yarpc-go_out=proto ${dir}*.proto
	done

grpc: grpc-install clean
	for dir in ./*/
	do
		protoc --proto_path=proto --go_out=plugins=grpc,paths=source_relative:proto ${dir}*.proto
	done