.PHONY: build test fmt install

build:
	cd go && go build ./...

install:
	cd go && go build -o /usr/local/bin/shave ./cmd/shave

test:
	cd go && go test ./...

fmt:
	cd go && gofmt -w .