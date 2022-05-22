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
	@rm api/swarm/*.pb.go

# go generate ./api/swarm
api/swarm/*.pb.go: ./api/swarm/swarm.proto
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative $<

$(OUT)/worker: internal/*.go api/swarm/*.pb.go cmd/worker/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/worker

$(OUT)/swarm: internal/*.go api/swarm/*.pb.go cmd/swarm/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/swarm

$(OUT)/ui: internal/*.go api/swarm/*.pb.go cmd/ui/*.go cmd/ui/*.html
	mkdir -p $(OUT)
	go build -o $@ ./cmd/ui
