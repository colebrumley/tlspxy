package main

import (
	"bytes"
	"io/ioutil"
	"net/http"

	log "github.com/Sirupsen/logrus"
)

// ProxyTransport is a custom http.Transport for use in the HTTP reverse proxy
type ProxyTransport struct {
	bytesTo, bytesFrom int64
	ShowContent        bool
	http.RoundTripper
}

// InterruptHandler writes info when an os signal is encountered.
func (t *ProxyTransport) InterruptHandler() {
	log.Infof("HTTP proxy sent %v bytes and received %v bytes", t.bytesTo, t.bytesFrom)
}

// RoundTrip invokes the underlying RoundTripper and captures data about the call
// on its way back to the client.
func (t *ProxyTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	log.Debugf("Calling: %s", req.URL.String())
	cl, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return
	}
	toTot := t.bytesTo + int64(len(cl))
	t.bytesTo = toTot
	resp, err = t.RoundTripper.RoundTrip(req)
	if err != nil {
		resp = nil
		return
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		resp = nil
		return
	}
	err = resp.Body.Close()
	if err != nil {
		resp = nil
		return
	}
	log.Debugf("Response: code=%v content-length=%v content-type=%v", resp.StatusCode, len(b), resp.Header["Content-Type"])

	// b = bytes.Replace(b, []byte("costume"), []byte("oversize novelty pantaloons"), -1)
	body := ioutil.NopCloser(bytes.NewReader(b))
	if t.ShowContent {
		log.Debugf("Content: %s", string(b))
	}
	resp.Body = body
	tot := t.bytesFrom + int64(len(b))
	t.bytesFrom = tot
	// resp.ContentLength = int64(len(b))
	// resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
	return
}
