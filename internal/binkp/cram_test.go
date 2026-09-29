package binkp

import (
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"testing"
)

func TestParseCRAMChallengeRecognizesOptLine(t *testing.T) {
	challenge, ok := parseCRAMChallenge("OPT CRAM-MD5-3a7f9c")
	if !ok || challenge != "3a7f9c" {
		t.Fatalf("parseCRAMChallenge = (%q, %v), want (3a7f9c, true)", challenge, ok)
	}
}

// TestParseCRAMChallengeAmongOtherOptions: a hub may offer CRAM-MD5
// next to other options on one OPT line (clrghouz sends "OPT
// CRAM-MD5-<hex> CRYPT") -- missing it there meant sending that hub
// the plaintext password.
func TestParseCRAMChallengeAmongOtherOptions(t *testing.T) {
	for _, arg := range []string{"OPT CRAM-MD5-1ad0 CRYPT", "OPT NR CRAM-MD5-1ad0", "OPT ND  CRAM-MD5-1ad0  CRYPT"} {
		if ch, ok := parseCRAMChallenge(arg); !ok || ch != "1ad0" {
			t.Errorf("parseCRAMChallenge(%q) = (%q, %v), want (1ad0, true)", arg, ch, ok)
		}
	}
}

func TestParseCRAMChallengeRejectsUnrelatedNulLines(t *testing.T) {
	cases := []string{"SYS Test BBS", "OPT SOMETHING-ELSE", "OPT NR CRYPT", "OPT CRAM-MD5-", "VER binkp/1.0", ""}
	for _, arg := range cases {
		if _, ok := parseCRAMChallenge(arg); ok {
			t.Fatalf("parseCRAMChallenge(%q) = ok, want not-ok", arg)
		}
	}
}

func TestParseCRAMResponseRoundTrip(t *testing.T) {
	digest, ok := parseCRAMResponse("CRAM-MD5-deadbeef")
	if !ok || digest != "deadbeef" {
		t.Fatalf("parseCRAMResponse = (%q, %v), want (deadbeef, true)", digest, ok)
	}
	if _, ok := parseCRAMResponse("plaintext-password"); ok {
		t.Fatal("parseCRAMResponse should reject a plain password")
	}
}

func TestCramDigestIsDeterministicAndKeyed(t *testing.T) {
	d1 := cramDigest("secret", "abc123")
	d2 := cramDigest("secret", "abc123")
	if d1 != d2 {
		t.Fatalf("cramDigest not deterministic: %q != %q", d1, d2)
	}
	if d3 := cramDigest("different", "abc123"); d3 == d1 {
		t.Fatal("cramDigest should differ for a different password")
	}
	if d4 := cramDigest("secret", "xyz789"); d4 == d1 {
		t.Fatal("cramDigest should differ for a different challenge")
	}
}

// TestCramDigestHMACsDecodedChallengeBytesNotHexText locks in the
// fix found by testing against a real answerer (binkterm-php): the
// challenge is transmitted as hex text but FTS-1027 HMACs the raw
// bytes it decodes to, not those hex characters themselves. An
// implementation that HMACs the hex string verbatim authenticates
// against nothing real (confirmed live) even though it's internally
// self-consistent, since both sides of a same-package test would
// otherwise agree with each other despite being wrong.
func TestCramDigestHMACsDecodedChallengeBytesNotHexText(t *testing.T) {
	password := "correct horse"
	challengeHex := "d590dcb8b93a07e8ab4f529c90dbf020"

	got := cramDigest(password, challengeHex)

	rawBytes, err := hex.DecodeString(challengeHex)
	if err != nil {
		t.Fatalf("hex.DecodeString: %v", err)
	}
	h := hmac.New(md5.New, []byte(password))
	h.Write(rawBytes)
	want := hex.EncodeToString(h.Sum(nil))

	if got != want {
		t.Fatalf("cramDigest = %q, want HMAC of the decoded bytes %q", got, want)
	}

	// The wrong-but-self-consistent version this replaced: HMAC of the
	// hex text itself. Assert it's different, so a regression back to
	// it would be caught even though it "looks" plausible.
	h2 := hmac.New(md5.New, []byte(password))
	h2.Write([]byte(challengeHex))
	hexTextDigest := hex.EncodeToString(h2.Sum(nil))
	if got == hexTextDigest {
		t.Fatal("cramDigest matched HMAC-of-hex-text -- regressed to hashing the challenge's ASCII form instead of its decoded bytes")
	}
}

func TestGenerateChallengeProducesDistinctHexStrings(t *testing.T) {
	a, err := generateChallenge()
	if err != nil {
		t.Fatalf("generateChallenge: %v", err)
	}
	b, err := generateChallenge()
	if err != nil {
		t.Fatalf("generateChallenge: %v", err)
	}
	if a == b {
		t.Fatal("expected two calls to generateChallenge to differ")
	}
	if len(a) != 32 { // 16 random bytes, hex-encoded
		t.Fatalf("challenge length = %d, want 32", len(a))
	}
}
