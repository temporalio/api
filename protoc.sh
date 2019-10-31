#!/bin/bash
protoc --go_out=plugins=grpc,paths=source_relative:. *.proto 
