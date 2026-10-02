GO ?= go
VERSION ?= dev
COMMIT ?= unknown
BUILD_DATE ?= unknown
BUILD_DIR ?= build

MODULE := github.com/sukumaar/yapp
LDFLAGS := -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
	-X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(MODULE)/internal/buildinfo.BuildDate=$(BUILD_DATE)

.PHONY: build
build:
	mkdir -p "$(BUILD_DIR)"
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$(BUILD_DIR)/yapp" ./cmd/yapp
