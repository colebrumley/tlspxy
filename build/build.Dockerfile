FROM golang:1.5.3
COPY ./* /go/src/tlspxy/
ENV CGO_ENABLED=0 \
  GO15VENDOREXPERIMENT=1
RUN set -e; cd /go/src/tlspxy; go get github.com/Masterminds/glide; glide install; go build -x -a -installsuffix cgo
