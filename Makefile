
GO    := GO15VENDOREXPERIMENT=1 go
pkgs   = $(shell $(GO) list ./... | grep -v /vendor/)

PREFIX                  ?= $(shell pwd)
BIN_DIR                 ?= $(shell pwd)
DOCKER_IMAGE_NAME       ?= samber/prometheus-inception-exporter
DOCKER_IMAGE_TAG        ?= $(shell cat VERSION)


all: format build test

style:
	@echo ">> checking code style"
	@! gofmt -d $(shell find . -path ./vendor -prune -o -name '*.go' -print) | grep '^'

test:
	@echo ">> running tests"
	@$(GO) test -short $(pkgs)

format:
	@echo ">> formatting code"
	@$(GO) fmt $(pkgs)

vet:
	@echo ">> vetting code"
	@$(GO) vet $(pkgs)

deps:
	@$(GO) get ./...

build: deps
	@echo ">> building binaries"
	@$(GO) build prometheus_inception_exporter.go

docker:
	@echo ">> building docker image"
	@docker build -t "$(DOCKER_IMAGE_NAME):$(DOCKER_IMAGE_TAG)" -t "$(DOCKER_IMAGE_NAME):latest" .

.PHONY: all style format build test vet tarball docker
