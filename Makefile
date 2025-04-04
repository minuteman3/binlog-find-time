.PHONY: build test clean

build:
	go build -o bin/binlog-find-time ./cmd

test:
	go test -v ./...

lint:
	./scripts/lint.sh
	
golangci-lint:
	golangci-lint run

clean:
	rm -rf bin/

install:
	go install ./cmd