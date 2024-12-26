#!/bin/bash

protoc --go_out=. --go-grpc_out=. --go_opt=paths=source_relative  *.proto