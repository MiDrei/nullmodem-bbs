package binkp

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// cramOptPrefix is the token an answerer advertises in an M_NUL "OPT"
// line to offer CRAM-MD5 authentication (FTS-1027), followed
// immediately by the hex challenge with no separator, e.g.
// "OPT CRAM-MD5-3a7f...".
const cramOptPrefix = "CRAM-MD5-"

// generateChallenge returns a random hex-encoded challenge for an
// answerer to send in its OPT line.
func generateChallenge() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("binkp: generate CRAM-MD5 challenge: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// cramDigest computes the hex-encoded HMAC-MD5 of challenge, keyed by
// password -- the value both sides compare to authenticate without
// sending password in the clear.
//
// challenge arrives as a hex string (that's how it's transmitted in
// the OPT line), but FTS-1027 HMACs the *raw bytes* it represents,
// not its ASCII hex text -- confirmed against a real answerer
// (binkterm-php) after an initial implementation that HMAC'd the hex
// string itself failed authentication. If challenge isn't valid hex
// (shouldn't happen with a spec-compliant peer), fall back to hashing
// it as literal text rather than erroring.
func cramDigest(password, challenge string) string {
	message, err := hex.DecodeString(challenge)
	if err != nil {
		message = []byte(challenge)
	}
	h := hmac.New(md5.New, []byte(password))
	h.Write(message)
	return hex.EncodeToString(h.Sum(nil))
}

// parseCRAMChallenge extracts the challenge from an M_NUL "OPT ..."
// argument offering CRAM-MD5, reporting ok=false if it doesn't. The
// OPT line may list several options in any order ("OPT CRAM-MD5-<hex>
// CRYPT" from clrghouz, "OPT NR CRAM-MD5-<hex>" and the like) -- an
// earlier version only accepted the challenge as the line's sole
// option, and silently sent such a hub the plaintext password.
func parseCRAMChallenge(arg string) (challenge string, ok bool) {
	fields := strings.Fields(arg)
	if len(fields) < 2 || fields[0] != "OPT" {
		return "", false
	}
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, cramOptPrefix) && len(f) > len(cramOptPrefix) {
			return strings.TrimPrefix(f, cramOptPrefix), true
		}
	}
	return "", false
}

// parseCRAMResponse extracts the digest from an M_PWD
// "CRAM-MD5-<hex>" argument, reporting ok=false if arg is a plain
// (non-CRAM) password instead.
func parseCRAMResponse(arg string) (digest string, ok bool) {
	if !strings.HasPrefix(arg, cramOptPrefix) {
		return "", false
	}
	return strings.TrimPrefix(arg, cramOptPrefix), true
}
