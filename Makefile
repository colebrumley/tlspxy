GO_BIN_NAME = tlspxy
GO_INSTALL_PATH = /usr/local/bin/tlspxy
DOCKER_IMAGE_NAME = elcolio/tlspxy:latest

dist: build docker

build: deps
	mkdir bin; \
	CGO_ENABLED=0 GO15VENDOREXPERIMENT=1 \
	go build -x -a -installsuffix cgo -o bin/$(GO_BIN_NAME)

install: build
	rm -f $(GO_INSTALL_PATH); \
	mv bin/$(GO_BIN_NAME) $(GO_INSTALL_PATH)

docker: build
	docker build -t $(DOCKER_IMAGE_NAME) .

deps:
	go get github.com/Masterminds/glide
	glide up

clean:
	rm -Rf vendor/ glide.lock tlspxy bin/
