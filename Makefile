.PHONY: all build test install clean fmt vet

PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

all: build

build:
	go build -o neocities ./cmd/neocities

test:
	go test ./...

install: build
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 755 neocities "$(DESTDIR)$(BINDIR)/neocities"

clean:
	rm -f neocities

fmt:
	gofmt -w .

vet:
	go vet ./...
