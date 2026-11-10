# rtk-go development targets. The product itself only needs `go build`.

BINARY ?= rtk-go
GOFLAGS ?= -mod=readonly -trimpath
VERSION ?= 0.1.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/ShawnMa123/token-trim/internal/buildinfo.Version=$(VERSION) \
           -X github.com/ShawnMa123/token-trim/internal/buildinfo.Commit=$(COMMIT)

.PHONY: all build test vet race check cross clean

all: check build

build:
	CGO_ENABLED=0 GOTOOLCHAIN=local go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/rtk-go

test:
	GOTOOLCHAIN=local go test $(GOFLAGS) ./...

vet:
	GOTOOLCHAIN=local go vet $(GOFLAGS) ./...

race:
	CGO_ENABLED=1 GOTOOLCHAIN=local go test -race $(GOFLAGS) ./...

check: test vet

cross:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOTOOLCHAIN=local go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/rtk-go-linux-amd64 ./cmd/rtk-go
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOTOOLCHAIN=local go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/rtk-go-windows-amd64.exe ./cmd/rtk-go

clean:
	rm -rf $(BINARY) dist
