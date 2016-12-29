#!/bin/bash -x

protoc -Iproto/ proto/sheep.proto --go_out=plugins=grpc:proto
go build github.com/dominichamon/flock/proto
go build -o bin/sheep github.com/dominichamon/flock/sheep
go build -o bin/shepherd github.com/dominichamon/flock/shepherd
go build -o bin/ui github.com/dominichamon/flock/ui
