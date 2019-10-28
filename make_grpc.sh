# go get -u google.golang.org/grpc

protoc --go_out=plugins=grpc,paths=source_relative:. *.proto 
