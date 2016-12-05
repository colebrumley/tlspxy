#!/bin/sh
# Does not work on OSX's native openssl installation!
cd "$(dirname "$0")"
exec 2>&1
openssl s_server \
    -tls1_2 \
    -accept 9999 \
    -cert ./certs/server.crt \
    -key ./certs/server.key \
    -CAfile ./certs/ca.crt
