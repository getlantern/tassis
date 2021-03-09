package identity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	keyPair, err := GenerateKeyPair()
	require.NoError(t, err)

	data := []byte("hello world")
	signature, err := keyPair.Private.Sign(data)
	require.NoError(t, err)

	stringPublicKey := keyPair.Public.String()
	publicKey, err := PublicKeyFromString(stringPublicKey)
	require.NoError(t, err)
	require.True(t, publicKey.Verify(data, signature))
}
