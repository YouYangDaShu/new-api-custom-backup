package service

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// MJ image proxy URLs are capability URLs: /mj/image/:id serves any task's
// image through the gateway's egress, so a bare (guessable/leakable) task id
// must not be enough to fetch another user's image. Every URL issued in task
// responses therefore carries an expiring HMAC keyed by CRYPTO_SECRET, and the
// handler rejects requests whose signature is missing, expired, or invalid.
// Clients keep working unchanged because they always receive freshly signed
// URLs from the task APIs; only stale copies of a URL stop working after the
// TTL (or after a restart when CRYPTO_SECRET is not pinned via env, since the
// fallback secret is per-process).
const mjImageURLTTL = time.Hour

func mjImageSignaturePayload(mjId string, expiresAt int64) string {
	return fmt.Sprintf("mj-image:%s:%d", mjId, expiresAt)
}

// MjImageSignature returns the hex HMAC that authorizes fetching mjId's image
// until expiresAt (unix seconds).
func MjImageSignature(mjId string, expiresAt int64) string {
	return common.GenerateHMAC(mjImageSignaturePayload(mjId, expiresAt))
}

// SignedMjImagePath returns the signed, expiring proxy path (without the
// server address) for an MJ task image.
func SignedMjImagePath(mjId string) string {
	expiresAt := time.Now().Add(mjImageURLTTL).Unix()
	return fmt.Sprintf("/mj/image/%s?ex=%d&sig=%s", mjId, expiresAt, MjImageSignature(mjId, expiresAt))
}

// VerifyMjImageSignature validates the expiring HMAC on an MJ image proxy
// request. Constant-time comparison; any failure must be treated as
// unauthorized by the caller.
func VerifyMjImageSignature(mjId, exParam, sigParam string) error {
	if mjId == "" || exParam == "" || sigParam == "" {
		return errors.New("missing image signature")
	}
	expiresAt, err := strconv.ParseInt(exParam, 10, 64)
	if err != nil {
		return errors.New("invalid image signature expiry")
	}
	if time.Now().Unix() > expiresAt {
		return errors.New("image signature expired")
	}
	expected := MjImageSignature(mjId, expiresAt)
	if !hmac.Equal([]byte(expected), []byte(sigParam)) {
		return errors.New("invalid image signature")
	}
	return nil
}
