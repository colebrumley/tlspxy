package sigv4

import (
	"encoding/xml"
	"errors"
	"net/http"
)

// awsError is the standard AWS REST-XML error envelope returned to clients.
type awsError struct {
	XMLName xml.Name `xml:"ErrorResponse"`
	Code    string   `xml:"Error>Code"`
	Message string   `xml:"Error>Message"`
}

// writeAWSError writes an AWS-style XML error response. Messages are generic and
// never include secret material, signatures, or internal error detail.
func writeAWSError(w http.ResponseWriter, status int, code, message string) {
	body, _ := xml.Marshal(awsError{Code: code, Message: message})
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

// verificationResponse maps a verification error to an HTTP status, AWS error
// code, generic client message, and a short audit reason. All verification
// failures return 403 (distinct from the 5xx used for gateway/credential
// failures) and never contact AWS.
func verificationResponse(err error) (status int, code, message, reason string) {
	switch {
	case errors.Is(err, ErrMalformedAuth):
		return http.StatusForbidden, "IncompleteSignatureException",
			"The request signature is missing or malformed.", "malformed_auth"
	case errors.Is(err, ErrUnknownKey):
		return http.StatusForbidden, "InvalidClientTokenId",
			"The security token included in the request is invalid.", "unknown_key"
	case errors.Is(err, ErrSignatureMismatch):
		return http.StatusForbidden, "SignatureDoesNotMatch",
			"The request signature does not match.", "signature_mismatch"
	case errors.Is(err, ErrClockSkew):
		return http.StatusForbidden, "RequestTimeTooSkewed",
			"The request date is outside the allowed clock-skew window.", "clock_skew"
	case errors.Is(err, ErrPayloadMismatch):
		return http.StatusForbidden, "XAmzContentSHA256Mismatch",
			"The provided payload hash does not match the request body.", "payload_mismatch"
	case errors.Is(err, ErrStreamingUnsupported):
		return http.StatusForbidden, "InvalidRequest",
			"Streaming (chunked) payload signing is not supported.", "streaming_unsupported"
	default:
		return http.StatusForbidden, "AccessDenied",
			"Request verification failed.", "verification_failed"
	}
}
