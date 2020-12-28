GOARCH = amd64

UNAME = $(shell uname -s)
fileExt = ""

ifndef OS
	ifeq ($(UNAME), Linux)
		OS = linux
	else ifeq ($(UNAME), Windows)
		OS = windows
	else ifeq ($(UNAME), Darwin)
		OS = darwin
	endif
endif

.DEFAULT_GOAL := all

all: fmt build start

build:
	GOOS=$(OS) GOARCH="$(GOARCH)" go build -o vault/plugins/eventstore-database-eventstore cmd/vault-plugin-database-eventstoredb/main.go

start:
	vault server -dev -log-level=debug -dev-root-token-id=root -dev-plugin-dir=./vault/plugins

enable:
	vault secrets enable database

clean:
	rm -f ./vault/plugins/mock

fmt:
	go fmt $$(go list ./...)

.PHONY: build clean fmt start enable