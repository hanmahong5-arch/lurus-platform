package auth

import (
	"crypto"
	_ "crypto/sha256" // register hash functions
	_ "crypto/sha512"
	"fmt"
)

// hashNameToCryptoHash maps a short hash name to the crypto.Hash constant.
func hashNameToCryptoHash(name string) (crypto.Hash, error) {
	switch name {
	case "sha256":
		return crypto.SHA256, nil
	case "sha384":
		return crypto.SHA384, nil
	case "sha512":
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("auth: unsupported hash %q", name)
	}
}
