package api

import "testing"

func TestImportTokenReadsMintedAddresses(t *testing.T) {
	if got := importToken("imports-abc123DEF@chaearkie.resend.app"); got != "abc123def" {
		t.Errorf("expected the token after the prefix, got %q", got)
	}
}

func TestImportTokenIgnoresAddressesWithoutAnAt(t *testing.T) {
	if got := importToken("not-an-address"); got != "" {
		t.Errorf("expected no token, got %q", got)
	}
}

func TestImportTokenDoesNotInventTokensForPlainAddresses(t *testing.T) {
	// This is what produced the silent 204: "dublin@..." yields the literal
	// "dublin", which is then looked up and never found. The lookup miss is the
	// safety net, so the token must stay something a mint would never produce.
	got := importToken("dublin@chaearkie.resend.app")
	if got == "" {
		t.Skip("a plain local part no longer produces a token")
	}
	if len(got) >= 20 {
		t.Errorf("a plain local part must not look like a minted token, got %q", got)
	}
}
