#!/bin/sh
cd "$(dirname "$0")"
exec 2>&1
SERVER_TLS_CERT=./certs/proxy.crt
SERVER_TLS_KEY=./certs/proxy.key
SERVER_TLS_CA=./certs/ca.crt
REMOTE_ADDR=localhost:9999
REMOTE_TLS_CERT=./certs/proxy.crt
REMOTE_TLS_KEY=./certs/proxy.key
REMOTE_TLS_CA=./certs/ca.crt
LOG_LEVEL=debug
LOG_CONTENTS="true"
../bin/tlspxy
