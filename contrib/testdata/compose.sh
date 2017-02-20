#!/bin/bash
cd "$(dirname "$0")" || exit 1

for f in ./compose_*.yml; do
    docker-compose -f $f up -d
    while ! nc -z localhost 9898; do   
        sleep 0.1 # wait for 1/10 of the second before check again
    done
    curl -v --fail -k https://localhost:9898/ || exit 1
    docker-compose -f $f down
done
