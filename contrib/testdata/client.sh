#!/bin/sh
cd "$(dirname "$0")"
exec 2>&1
echo "x" | timeout 1 \
    openssl s_client \
        -tls1_2 \
        -connect localhost:9898 \
        -cert ./certs/client.crt \
        -key ./certs/client.key \
        -CAfile ./certs/ca.crt
