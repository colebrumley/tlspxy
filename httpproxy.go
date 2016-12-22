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
	valTo := int(t.bytesTo / 1024)
	valFrom := int(t.bytesFrom / 1024)
	log.Infof("HTTP proxy sent %v Kb and received %v Kb", valTo, valFrom)
}

// RoundTrip invokes the underlying RoundTripper and captures data about the call
// on its way back to the client.
func (t *ProxyTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	log.Debugf("Calling: %s", req.URL.String())
	toTot := t.bytesTo + req.ContentLength
	t.bytesTo = toTot
	resp, err = t.RoundTripper.RoundTrip(req)
	if err != nil {
		resp = nil
		return
	}
	log.Debugf("Response: code=%v content-length=%v content-type=%v", resp.StatusCode, resp.ContentLength, resp.Header["Content-Type"])
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
