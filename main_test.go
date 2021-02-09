package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRedisTLSURLPassword(t *testing.T) {
	useTLS, pass, addr, err := parseRedisURL("rediss://:password@localhost:6379")
	require.NoError(t, err)
	require.True(t, useTLS)
	require.Equal(t, "password", pass)
	require.Equal(t, "localhost:6379", addr)
}

func TestParseRedisNoTLSURLNoPassword(t *testing.T) {
	useTLS, pass, addr, err := parseRedisURL("redis://:@localhost:6379")
	require.NoError(t, err)
	require.False(t, useTLS)
	require.Equal(t, "", pass)
	require.Equal(t, "localhost:6379", addr)
}
