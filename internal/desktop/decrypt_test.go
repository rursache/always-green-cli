package desktop

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

func TestDecryptCookieRoundTrip(t *testing.T) {
	password := []byte("test-password")
	plain := []byte("xoxd-hello-cookie-value")
	enc := encryptV10(t, plain, password, 1003)
	got, err := decryptCookie(enc, password, 1003)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q", got)
	}
}

func TestStripDomainHash(t *testing.T) {
	body := append(bytes.Repeat([]byte{0xab}, 32), []byte("xoxd-real")...)
	if got := string(stripDomainHash(body)); got != "xoxd-real" {
		t.Fatalf("got %q", got)
	}
}

func TestLooksLikeXoxc(t *testing.T) {
	if looksLikeXoxc("xoxc-short") {
		t.Fatal("short token should be rejected")
	}
	long := "xoxc-1111111111111-2222222222222-3333333333333-" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if !looksLikeXoxc(long) {
		t.Fatal("long token should pass")
	}
}

func encryptV10(t *testing.T, plain, password []byte, rounds int) []byte {
	t.Helper()
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	src := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	key := pbkdf2.Key(password, []byte("saltysalt"), rounds, 16, sha1.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	iv := bytes.Repeat([]byte{' '}, 16)
	out := make([]byte, len(src))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, src)
	return append([]byte("v10"), out...)
}

// With two Slack installs the keychain holds two Safe Storage items; the
// first one found may belong to the other profile, so the wrong password must
// simply fail rather than be treated as authoritative
func TestDecryptCookieRejectsWrongPassword(t *testing.T) {
	enc := encryptV10(t, []byte("xoxd-hello-cookie-value"), []byte("right-password"), 1003)
	if _, err := decryptCookie(enc, []byte("wrong-password"), 1003); err == nil {
		t.Fatal("a wrong password must not decrypt")
	}
	got, err := decryptCookie(enc, []byte("right-password"), 1003)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "xoxd-hello-cookie-value" {
		t.Fatalf("got %q", got)
	}
}

func TestDecryptCookieRejectsUnknownPrefix(t *testing.T) {
	if _, err := decryptCookie([]byte("v99somethingelse"), []byte("pw"), 1003); err == nil {
		t.Fatal("an unknown prefix must be rejected")
	}
	if _, err := decryptCookie([]byte("ab"), []byte("pw"), 1003); err == nil {
		t.Fatal("a truncated blob must be rejected")
	}
}
