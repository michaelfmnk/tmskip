VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test integration check snapshot

# ./tmskip with the version stamped in
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o tmskip ./cmd/tmskip

# into $(go env GOPATH)/bin, a stable path for the schedule
install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/tmskip

test:
	go test ./...

# real xattrs + tmutil, only in temp dirs
integration:
	go test -tags integration ./...

# what CI runs
check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go vet -tags integration ./...
	go test ./...

# local release build, no signing
snapshot:
	goreleaser release --snapshot --clean --skip=sign,notarize,publish
