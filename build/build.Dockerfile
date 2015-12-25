FROM golang:1.5.2
COPY ./* /go/src/tlspxy/
ENV CGO_ENABLED=0
RUN set -e; cd /go/src/tlspxy; go get -d; go build -x -a -installsuffix cgo