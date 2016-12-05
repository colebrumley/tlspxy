#!/bin/sh
cd "$(dirname "$0")"
exec 2>&1
../../bin/tlspxy \
    -log-level=debug \
    -log-contents=true \
    -remote-addr=localhost:9999 \
    -remote-tls-cert=./certs/proxy.crt \
    -remote-tls-key=./certs/proxy.key \
    -remote-tls-ca=./certs/ca.crt \
    -server-tls-cert=./certs/proxy.crt \
    -server-tls-key=./certs/proxy.key \
    -server-tls-ca=./certs/ca.crt