# ghapk — build helpers.  Everything is plain Go standard library: no downloads.
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X ghapk/internal/cli.Version=$(VERSION)
GOFLAGS  = -trimpath
export CGO_ENABLED = 0

.PHONY: build install test vet fmt android linux mac release demo clean

build:            ## binary for this machine -> ./ghapk
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ghapk .

install:          ## into $(go env GOPATH)/bin (or $PREFIX/bin on Termux: make install DEST=$$PREFIX/bin)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(or $(DEST),$(shell go env GOPATH)/bin)/ghapk .

android:          ## arm64 Android binary for Termux -> dist/ghapk-android-arm64
	GOOS=android GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/ghapk-android-arm64 .

linux:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/ghapk-linux-amd64 .

mac:
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/ghapk-macos-arm64 .

release: android linux mac   ## every binary + checksums
	cd dist && sha256sum ghapk-* > SHA256SUMS

test:
	go test ./...

vet:
	go vet ./... && test -z "$$(gofmt -l .)"

fmt:
	gofmt -w .

demo: build       ## try it with a fake GitHub and a fake phone (needs python3, openssl)
	sh scripts/demo.sh

clean:
	rm -rf ghapk dist
