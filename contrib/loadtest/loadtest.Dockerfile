FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /loadtest ./cmd/loadtest

FROM alpine:latest
COPY --from=builder /loadtest /loadtest
COPY --from=builder /src/contrib/testdata/certs/ /certs/
ENTRYPOINT ["/loadtest", "-certdir", "/certs"]
