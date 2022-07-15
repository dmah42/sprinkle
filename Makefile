.PHONY: all
all: worker run ui

OUT=bin

.PHONY: worker
worker: $(OUT)/worker

.PHONY: run
run: $(OUT)/run

.PHONY: ui
ui: $(OUT)/ui

.PHONY: clean
clean:
	@rm $(OUT)/*
	@rm api/sprinkle/*.pb.go

api/sprinkle/*.pb.go: ./api/sprinkle/sprinkle.proto
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative $<

$(OUT)/worker: internal/*.go api/sprinkle/*.pb.go cmd/worker/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/worker

$(OUT)/run: internal/*.go api/sprinkle/*.pb.go cmd/run/*.go
	mkdir -p $(OUT)
	go build -o $@ ./cmd/run

$(OUT)/ui: internal/*.go api/sprinkle/*.pb.go cmd/ui/*.go cmd/ui/*.html $(OUT)/favicon.ico $(OUT)/logo.png
	mkdir -p $(OUT)
	go build -o $@ ./cmd/ui

$(OUT)/logo.png: assets/donut-with-sprinkles.svg
	convert -density 1200 -resize 200x200 -background None $< $@

$(OUT)/favicon.ico: assets/donut-with-sprinkles.svg
	convert -density 1200 -resize 16x16 -background None $< $@