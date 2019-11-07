.PHONY: grpc yarpc clean yarpc-install grpc-install

yarpc-install:
	go get -u github.com/gogo/protobuf/protoc-gen-gogoslick
	go get -u go.uber.org/yarpc/encoding/protobuf/protoc-gen-yarpc-go

grpc-install:
	go get -u github.com/golang/protobuf/protoc-gen-go
	go get -u google.golang.org/grpc

clean:
	rm -f *.go

yarpc: yarpc-install clean
	protoc --proto_path=. --gogoslick_out=paths=source_relative:. *.proto 
	protoc --proto_path=. --yarpc-go_out=. *.proto 

grpc: grpc-install clean
	protoc --proto_path=. --go_out=plugins=grpc,paths=source_relative:. *.proto 
