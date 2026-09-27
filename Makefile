.PHONY: all build test clean run

BINARY=bin/tunnelchat

all: build

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/tunnelchat

test:
	go test -v ./...

clean:
	rm -rf bin/tunnelchat tunnelchat.xml fakechat.xml

run: build
	./$(BINARY)
