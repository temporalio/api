# go get -u github.com/gogo/protobuf/protoc-gen-gogoslick
# go get -u go.uber.org/yarpc/encoding/protobuf/protoc-gen-yarpc-go

protoc --gogoslick_out=paths=source_relative:. *.proto 
protoc --yarpc-go_out=. *.proto 
