# tlspxy - A small TLS termination proxy

`tlspxy` is A small TCP-based TLS termination proxy that supports x509 cert verification on either the proxy or upstream servers. It is also capable of TLS passthrough, so `tlspxy` will handle verification but still pass the client's cert upstream for things like cert CN auth.

## Still working on
`tlspxy` is a work in progress. Currently, it can handle TLS on the proxy or upstream sides and verification, but does not do TLS passthrough.