package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestError(t *testing.T) {
	orig := &Error{
		Name:        "an_error",
		Description: "something went wrong",
	}

	b, err := proto.Marshal(orig)
	require.NoError(t, err)
	roundTripped := &Error{}
	err = proto.Unmarshal(b, roundTripped)
	require.NoError(t, err)
	require.EqualValues(t, orig.Error(), roundTripped.Error())

	makeTypedError := func() error {
		return orig
	}
	require.EqualValues(t, orig, TypedError(makeTypedError()))

	makeUntypedError := func() error {
		return errors.New("I'm an error")
	}
	require.EqualValues(t, ErrUnknown.WithDescription(makeUntypedError().Error()).Error(), TypedError(makeUntypedError()).Error())
}
