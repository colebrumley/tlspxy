FROM        alpine:latest
MAINTAINER  Cole Brumley <github.com/colebrumley>
RUN         apk add --update ca-certificates; rm -Rf /var/cache/apk/*
COPY        bin/tlspxy /sbin/tlspxy
ENTRYPOINT  ["/sbin/tlspxy"]
