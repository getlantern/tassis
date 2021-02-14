package identity

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
)

var (
	djbType = []byte{0x05}

	userIDEncoding = base32.NewEncoding("ybndrfg8ejkmcpqxot1uw2sza345h769").WithPadding(base32.NoPadding)
)

// A UserID is a unique identifier for a user that's also usable as a Signal public
// identity key.
//
// This is a 33 byte value whose first byte indicates the key type for Signal
// (at the moment this is always 0x05 for DJB_TYPE). The remaining 32 bytes are
// an ed25519 public key.
type UserID []byte

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

// Returns a UserID that's also usable as a Signal public identity key.
func (pub PublicKey) UserID() UserID {
	return append(djbType, pub...)
}

// Returns the PublicKey portion of this userID
func (id UserID) PublicKey() PublicKey {
	return PublicKey(id[1:])
}

func (id UserID) String() string {
	return userIDEncoding.EncodeToString(id)
}

func UserIDFromString(id string) (UserID, error) {
	return userIDEncoding.DecodeString(id)
}
