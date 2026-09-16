GO ?= go
export GO111MODULE := on
export CGO_ENABLED := 0

.PHONY: all test
all:
	$(GO) build -trimpath -o main main.go

test:
	$(GO) test ./...
