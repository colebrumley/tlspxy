FROM        alpine:latest
MAINTAINER  Cole Brumley <github.com/colebrumley>
RUN         apk add --update ca-certificates; rm -Rf /var/cache/apk/*
COPY        bin/tlspxy_linux_x86_64 /sbin/tlspxy
ENTRYPOINT  ["/sbin/tlspxy"]
