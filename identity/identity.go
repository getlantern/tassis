package identity

import (
	"crypto/ed25519"

	"github.com/getlantern/tassis/encoding"
)

// PublicKey is a 32 byte Curve25519 (x25519) public key
type PublicKey []byte

// Verifies the given signature on the given data using the Ed25519 version of this Curve25519
// Public Key
func (pub PublicKey) Verify(data, signature []byte) bool {
	// TODO: the below doesn't always work in the unit test, probably because the sign is lost in x25519
	var key [32]byte
	copy(key[:], pub)

	// below code from https://stackoverflow.com/questions/62586488/how-do-i-sign-a-curve25519-key-in-golang
	key[31] &= 0x7F

	/* Convert the Curve25519 public key into an Ed25519 public key.  In
	particular, convert Curve25519's "montgomery" x-coordinate into an
	Ed25519 "edwards" y-coordinate:
	ed_y = (mont_x - 1) / (mont_x + 1)
	NOTE: mont_x=-1 is converted to ed_y=0 since fe_invert is mod-exp
	Then move the sign bit into the pubkey from the signature.
	*/

	var edY, one, montX, montXMinusOne, montXPlusOne FieldElement
	FeFromBytes(&montX, &key)
	FeOne(&one)
	FeSub(&montXMinusOne, &montX, &one)
	FeAdd(&montXPlusOne, &montX, &one)
	FeInvert(&montXPlusOne, &montXPlusOne)
	FeMul(&edY, &montXMinusOne, &montXPlusOne)

	var A_ed [32]byte
	FeToBytes(&A_ed, &edY)

	A_ed[31] |= signature[63] & 0x80
	signature[63] &= 0x7F

	var sig = make([]byte, 64)
	var aed = make([]byte, 32)

	copy(sig, signature[:])
	copy(aed, A_ed[:])

	return ed25519.Verify(aed, data, signature)
}

func (pub PublicKey) String() string {
	return encoding.HumanFriendlyBase32Encoding.EncodeToString(pub)
}

func PublicKeyFromString(id string) (PublicKey, error) {
	return encoding.HumanFriendlyBase32Encoding.DecodeString(id)
}
