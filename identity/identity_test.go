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

	userID := keyPair.Public.UserID()
	stringUserID := userID.String()
	userID2, err := UserIDFromString(stringUserID)
	require.NoError(t, err)
	publicKey := userID2.PublicKey()
	require.True(t, publicKey.Verify(data, signature))
}
