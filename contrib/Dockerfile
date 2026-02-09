# Build stage
FROM golang:1.24-alpine AS builder

ARG VERSION=0.1.0
ARG COMMIT_ID=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.AppVersion=${VERSION} -X main.CommitID=${COMMIT_ID}" \
    -x -v -a -installsuffix cgo \
    -o /usr/local/bin/tlspxy

# Final stage
FROM alpine:3.21

LABEL maintainer="Cole Brumley"
LABEL version="${VERSION}"
LABEL description="TLS termination proxy"

RUN apk add --no-cache ca-certificates

COPY --from=builder /usr/local/bin/tlspxy /sbin/tlspxy

ENTRYPOINT ["/sbin/tlspxy"]
