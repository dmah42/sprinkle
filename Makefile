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
	@rm api/swarm/swarm_grpc.pb.go

api/swarm/swarm_grpc.pb.go: ./api/swarm/*.proto
	go generate ./api/swarm

$(OUT)/worker: internal/*.go api/swarm/swarm_grpc.pb.go cmd/worker/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/worker

$(OUT)/swarm: internal/*.go api/swarm/swarm_grpc.pb.go cmd/swarm/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/swarm

$(OUT)/ui: internal/*.go api/swarm/swarm_grpc.pb.go cmd/ui/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/ui
