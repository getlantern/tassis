package identity

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"

	"github.com/getlantern/tassis/encoding"
)

// PublicKey is an ed25519.PublicKey that is 32 bytes long
type PublicKey ed25519.PublicKey

// PrivateKey is an ed25519.PrivateKey
type PrivateKey ed25519.PrivateKey

// KeyPair is an ed25519 key pair
type KeyPair struct {
	Public  PublicKey
	Private PrivateKey
}

// GenerateKeyPair generates a new randomg KeyPair
func GenerateKeyPair() (*KeyPair, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &KeyPair{
		Public:  PublicKey(public),
		Private: PrivateKey(private),
	}, nil
}

// Signs the given data and returns the resulting signature
func (priv PrivateKey) Sign(data []byte) ([]byte, error) {
	return ed25519.PrivateKey(priv).Sign(rand.Reader, data, crypto.Hash(0))
}

// Verifies the given signature on the given data using this PublicKey
func (pub PublicKey) Verify(data, signature []byte) bool {
	return ed25519.Verify(ed25519.PublicKey(pub), data, signature)
}

func (pub PublicKey) String() string {
	return encoding.HumanFriendlyBase32Encoding.EncodeToString(pub)
}

func PublicKeyFromString(id string) (PublicKey, error) {
	return encoding.HumanFriendlyBase32Encoding.DecodeString(id)
}
