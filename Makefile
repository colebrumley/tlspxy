GO_BIN_NAME = tlspxy
GO_INSTALL_PATH = /usr/local/sbin/tlspxy
DOCKER_IMAGE_NAME = elcolio/tlspxy:latest

dist: deps build

build:
	mkdir bin; \
	export CGO_ENABLED=0 GO15VENDOREXPERIMENT=1; \
	go build -x -a -installsuffix cgo -o bin/$(GO_BIN_NAME)

install: build
	rm -f $(GO_INSTALL_PATH); \
	mv bin/$(GO_BIN_NAME) $(GO_INSTALL_PATH)

test:
	go test -v

docker:
	docker pull golang:latest
	docker run -it --rm \
		-v "$$(pwd):/go/src/github.com/colebrumley/tlspxy" \
		golang:latest bash -c \
		"cd /go/src/github.com/colebrumley/tlspxy && make && mv bin/tlspxy bin/tlspxy_linux_x64"
	docker build -t $(DOCKER_IMAGE_NAME) -f contrib/Dockerfile .

deps:
	command -v glide || go get github.com/Masterminds/glide
	glide update
	glide install

clean:
	rm -Rf vendor/ glide.lock tlspxy bin/
