#!/bin/bash
set -x
BASEDIR=$(cd $(dirname $0)/..; pwd)

cd $BASEDIR

if ! [[ $(which docker > /dev/null | echo $?) ]] || ! [[ $(docker ps > /dev/null | echo $?) ]]; then
	echo "Couldn't find docker!"
	exit 1
fi

rm -Rf .buildcache; mkdir .buildcache
docker build -f build/build.Dockerfile -t tlspxy-tmp .
docker run --name tlspxy-binary tlspxy-tmp
docker cp tlspxy-binary:/go/src/tlspxy/tlspxy .buildcache/
docker rm -f tlspxy-binary; docker rmi -f tlspxy-tmp


if ! [[ -f .buildcache/tlspxy ]]; then
	echo "Failed to build binary!"
	exit 1
fi

docker build -t elcolio/tlspxy -f build/Dockerfile .

rm -Rf .buildcache
