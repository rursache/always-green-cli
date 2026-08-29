package desktop

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"errors"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

var errDecrypt = errors.New("could not decrypt Slack cookie")

func decryptCookie(enc, password []byte, rounds int) ([]byte, error) {
	if len(enc) < 4 {
		return nil, errDecrypt
	}
	prefix := string(enc[:3])
	body := enc[3:]
	switch prefix {
	case "v10", "v11":
		plain, err := decryptCBC(body, password, rounds)
		if err != nil {
			return nil, err
		}
		return stripDomainHash(plain), nil
	default:
		return nil, fmt.Errorf("%w: unknown prefix %q", errDecrypt, prefix)
	}
}

func decryptCBC(value, password []byte, rounds int) ([]byte, error) {
	if len(value) == 0 || len(value)%aes.BlockSize != 0 {
		return nil, errDecrypt
	}
	key := pbkdf2.Key(password, []byte("saltysalt"), rounds, 16, sha1.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := bytes.Repeat([]byte{' '}, aes.BlockSize)
	out := make([]byte, len(value))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, value)
	n := int(out[len(out)-1])
	if n <= 0 || n > aes.BlockSize || n > len(out) {
		return nil, errDecrypt
	}
	for _, b := range out[len(out)-n:] {
		if int(b) != n {
			return nil, errDecrypt
		}
	}
	return out[:len(out)-n], nil
}

func stripDomainHash(value []byte) []byte {
	if len(value) > 32 && bytes.HasPrefix(value[32:], []byte("xoxd-")) {
		return value[32:]
	}
	return value
}
