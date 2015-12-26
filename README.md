# tlspxy - A small TLS termination proxy

`tlspxy` is A small TCP-based TLS termination proxy that supports x509 cert verification on either the proxy or upstream servers. It is also capable of TLS passthrough, so `tlspxy` will handle verification but still pass the client's cert upstream for things like cert CN auth.

## Build
The build is Docker-based. Provided you have docker installed already, run `build/build.sh`. You will end up with an image tagged as `elcolio/tlspxy:latest` which contains a statically linked linux/x64 binary. If you just want the binary, run the following commands to copy it into your local directory (I'm using the `docker cp` method versus mounting volumes since that works with remote `docker-machine` instances):

```bash
docker run --name tmp elcolio/tlspxy
docker cp tmp:/sbin/tlspxy .
docker rm tmp
sudo mv tlspxy /usr/sbin/ # or wherever
```

## Run
`tlspxy` was meant for running in a Docker container, so several of the environment variables have generic names that could conflict with other applications. The binary itself is happy anywhere golang is, including non-glibc distros like Alpine linux.

See `docs/configuration.md` for available options and configuration methods.

### TLS warning
Golang's TLS implementation is [pretty strict](http://www.bite-code.com/2015/06/25/tls-mutual-auth-in-golang/). As a result, you may have to occasionally turn verification off for public websites. For example, attempting to proxy to `google.com:443` with verification on will error with something like:

```
WARN[0039] Connection #001 Remote connection failed: x509: cannot validate certificate for 216.58.219.238 because it doesn't contain any IP SANs
```

In short, if verification is on _everything_ will be verified. IP addresses, SANs, DNS names, all of it. To run a proxy to google with the containerized binary, run `docker run -it --rm -p 9898:9898 elcolio/tlspxy -remote-tls-verify false`.

## Still working on
`tlspxy` is a work in progress. Currently, it can handle TLS on the proxy or upstream sides and do verification, but does not do TLS passthrough.