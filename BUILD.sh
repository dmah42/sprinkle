#!/bin/bash +x

GH=github.com/dominichamon/swarm
OUT=bin

echo "building proto"
protoc -Iproto/ ./proto/swarm.proto --go_out=plugins=grpc:proto
go build $GH/proto

for b in worker swarm ui; do
  echo "building $b"
  go build -o $OUT/$b $GH/$b
done
