package api

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestImportTokenPreservesCase(t *testing.T) {
	// The regression: tokens are base64url and mixed case, so lowercasing the
	// address made every minted forwarding address fail to resolve.
	const token = "LXfmczv6nHyVpRr29ptm4evvZk-P570n"
	if got := importToken("imports-" + token + "@chaearkie.resend.app"); got != token {
		t.Errorf("importToken lost the token's case: got %q, want %q", got, token)
	}
}

func TestImportTokenAcceptsAnyCasingOfThePrefix(t *testing.T) {
	const token = "LXfmczv6nHyVpRr29ptm4evvZk-P570n"
	for _, prefix := range []string{"imports-", "Imports-", "IMPORTS-"} {
		if got := importToken(prefix + token + "@chaearkie.resend.app"); got != token {
			t.Errorf("prefix %q should be matched case-insensitively, got %q", prefix, got)
		}
	}
}

func TestImportTokenRoundTripsAGeneratedToken(t *testing.T) {
	token, err := newImportToken()
	if err != nil {
		t.Fatalf("newImportToken: %v", err)
	}
	if got := importToken("imports-" + token + "@example.test"); got != token {
		t.Errorf("a freshly minted token must survive the round trip: got %q, want %q", got, token)
	}
	if _, err := base64.RawURLEncoding.DecodeString(token); err != nil {
		t.Errorf("tokens are expected to be base64url: %v", err)
	}
	if strings.ToLower(token) == token {
		t.Log("token happened to be all lowercase; the case-preservation test above is the real guard")
	}
}

func TestImportTokenRejectsAddressesWithoutThePrefix(t *testing.T) {
	// This produced the first silent 204: "dublin@..." used to yield the literal
	// "dublin", which was then looked up and never found.
	for _, address := range []string{"dublin@chaearkie.resend.app", "not-an-address", "imports-@example.test", "@example.test"} {
		if got := importToken(address); got != "" {
			t.Errorf("importToken(%q) should yield no token, got %q", address, got)
		}
	}
}
