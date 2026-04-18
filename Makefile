.PHONY: run build dev test clean

run:
	go run cmd/fable/main.go

build:
	go build -o bin/fable cmd/fable/main.go

dev:
	go run cmd/fable/main.go

test:
	go test ./...

clean:
	rm -rf bin/
