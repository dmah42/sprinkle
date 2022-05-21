.PHONY: all
all: worker swarm ui

OUT=bin

.PHONY: worker
worker: $(OUT)/worker

.PHONY: swarm
swarm: $(OUT)/swarm

.PHONY: ui
ui: $(OUT)/ui

.PHONY: clean
clean:
	@rm $(OUT)/*
	@rm proto/swarm_grpc.pb.go

proto/swarm_grpc.pb.go: proto/swarm.proto
	protoc --go-grpc_out=./ --go-grpc_opt=paths=source_relative $<

$(OUT)/worker: internal/*.go proto/swarm_grpc.pb.go cmd/worker/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/worker

$(OUT)/swarm: internal/*.go proto/swarm_grpc.pb.go cmd/swarm/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/swarm

$(OUT)/ui: internal/*.go proto/swarm_grpc.pb.go cmd/ui/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/ui
