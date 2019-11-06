#!/bin/bash

rm -f *.go
protoc --go_out=plugins=grpc,paths=source_relative:. *.proto 
