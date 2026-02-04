BINARY=heraldev

.PHONY: build release obfuscate test clean

build:
	go build -o $(BINARY) ./cmd/herald

release:
	go build -ldflags="-s -w" -o herald ./cmd/herald

obfuscate:
	garble -literals -tiny build -ldflags="-s -w" -o herald ./cmd/herald

test:
	go test ./...

clean:
	rm -f $(BINARY)
