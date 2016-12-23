GO_REPO = github.com/colebrumley/tlspxy
GO_INSTALL_PATH = /usr/sbin/tlspxy
DOCKER_IMAGE_NAME = elcolio/tlspxy:latest
VERSION = 0.1.0
COMMIT_ID = $$(git log | head -n 1 | awk '{print $$2}')

# the go binary will be named tlxpxy_<os>_<arch>
GO_BIN_NAME = tlspxy_$$(uname -s -m | tr '[:upper:]' '[:lower:]' | tr ' ' '_')

# Setup the -ldflags option for go build here, interpolate the variable values
LDFLAGS=-ldflags "-X main.AppVersion=$(VERSION) -X main.CommitID=$(COMMIT_ID)"

# The cgo suffix is for as-true-as-possible static compliation
EXTRAFLAGS=-x -v -a -installsuffix cgo

dist: deps build

build:
	mkdir bin; \
	export CGO_ENABLED=0 GO15VENDOREXPERIMENT=1; \
	go build $(LDFLAGS) $(EXTRAFLAGS) -o bin/$(GO_BIN_NAME)

install: build
	rm -f $(GO_INSTALL_PATH); \
	mv bin/$(GO_BIN_NAME) $(GO_INSTALL_PATH)

test:
	go test -v

docker:
	docker pull golang:latest
	docker run -it --rm -v "$$(pwd):/go/src/$(GO_REPO)" \
		golang:latest bash -c "cd /go/src/$(GO_REPO) && make"
	docker build -t $(DOCKER_IMAGE_NAME) -f contrib/Dockerfile .

deps:
	command -v glide || go get github.com/Masterminds/glide
	glide update
	glide install

clean:
	rm -Rf vendor/ glide.lock tlspxy bin/
