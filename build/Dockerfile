FROM alpine
RUN apk add --update ca-certificates; rm -Rf /var/cache/apk/*
COPY .buildcache/tlspxy /sbin/tlspxy
ENTRYPOINT ["/sbin/tlspxy"]
