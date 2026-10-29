BINARY  := ghapk
VERSION := 4.3.0
LDFLAGS := -s -w -X main.appVersion=$(VERSION)
GOFLAGS := -trimpath

.PHONY: all build run test vet fmt clean termux release upx

all: build

## build: static binary for the current machine
build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BINARY) .

## run: build + open the TUI
run: build
	./$(BINARY)

## test: unit + integration tests
test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

## termux: android/arm64 binary for your phone
termux:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 \
		go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/$(BINARY)-android-arm64 .

## release: every target, stripped, with sha256sums
release: clean
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/$(BINARY)-linux-arm64 .
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/$(BINARY)-android-arm64 .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/$(BINARY)-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/$(BINARY)-darwin-amd64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o bin/$(BINARY)-windows-amd64.exe .
	cd bin && sha256sum * > sha256sums.txt && ls -lh

## upx: compress everything in bin/ (needs upx installed)
upx: release
	upx --best --lzma bin/$(BINARY)-* || true

clean:
	rm -rf bin $(BINARY)
