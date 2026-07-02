GO_REPO = github.com/colebrumley/tlspxy
GO_INSTALL_PATH = /usr/sbin/tlspxy
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT_ID = $$(git log | head -n 1 | awk '{print $$2}')

# the go binary will be named tlxpxy_<os>_<arch>
GO_BIN_NAME = tlspxy_$$(uname -s -m | tr '[:upper:]' '[:lower:]' | tr ' ' '_')

# Setup the -ldflags option for go build here, interpolate the variable values
LDFLAGS=-ldflags "-X main.AppVersion=$(VERSION) -X main.CommitID=$(COMMIT_ID)"

# The cgo suffix is for as-true-as-possible static compliation
EXTRAFLAGS=-x -v -a -installsuffix cgo

dist: deps build

build:
	mkdir -p bin; \
	export CGO_ENABLED=0; \
	go build $(LDFLAGS) $(EXTRAFLAGS) -o bin/$(GO_BIN_NAME)

install: build
	rm -f $(GO_INSTALL_PATH); \
	mv bin/$(GO_BIN_NAME) $(GO_INSTALL_PATH)

test:
	go test -v -race ./...

deps:
	go mod tidy

clean:
	rm -Rf tlspxy bin/

loadtest:
	go build -o bin/loadtest ./cmd/loadtest

bench:
	go test -bench=. -benchtime=10s -run=^$$ ./internal/proxy/

loadtest-docker:
	docker compose -f contrib/loadtest/docker-compose.yml up --build --abort-on-container-exit
