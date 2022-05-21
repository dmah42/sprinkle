#!/bin/bash +x

GH=github.com/dominichamon/swarm
OUT=bin

echo "building proto"
protoc --go-grpc_out=./ --go-grpc_opt=paths=source_relative proto/swarm.proto
go build $GH/proto

for b in worker swarm ui; do
  echo "building command $b"
  go build -o $OUT/$b $GH/cmd/$b
done
