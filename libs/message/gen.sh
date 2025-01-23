#!/bin/bash

protoc --go_out=. --go-grpc_out=.  --go_opt=paths=source_relative  *.proto

# edit .pb.go file to add `ssz-size` and `ssz-max`
sszgen --path . 