VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-all clean

build:
	go build -ldflags="$(LDFLAGS)" -o lore-watch-light .

build-all: clean
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/lore-watch-light-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/lore-watch-light-darwin-amd64 .
	GOOS=linux  GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/lore-watch-light-linux-amd64 .
	GOOS=linux  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/lore-watch-light-linux-arm64 .

clean:
	rm -f lore-watch-light
	rm -rf dist
