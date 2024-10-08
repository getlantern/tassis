# tassis [![Build and Test](https://github.com/getlantern/tassis/workflows/Build%20and%20Test/badge.svg)](https://github.com/getlantern/tassis/actions?query=workflow%3A%22Build+and+Test%22)&nbsp;[![Coverage Status](https://coveralls.io/repos/github/getlantern/tassis/badge.png?branch=main&t=PPGsvs)](https://coveralls.io/github/getlantern/tassis?branch=main)&nbsp;[![Go Reference](https://pkg.go.dev/badge/github.com/getlantern/tassis.svg)](https://pkg.go.dev/github.com/getlantern/tassis)

<!-- &nbsp;[![Sourcegraph](https://sourcegraph.com/github.com/getlantern/tassis/-/badge.svg)](https://sourcegraph.com/github.com/getlantern/tassis?badge) -->

See [testsupport/testsupport.go](testsupport/testsupport.go) for a fairly full usage example that illustrates the message exchange pattern.

Comprehensive user documentation, including message formats, is in [docs](docs/README.md).

## Redis Clustering
tassis is designed to run on [Redis Cluster](https://redis.io/topics/cluster-spec). That means that we need to pay attention to key distribution (see section "Keys hash tags" of the [Redis Cluster Spec](https://redis.io/topics/cluster-spec)).

### Global Keys
Global keys like the index of all chat numbers us the hash tag `{global}`.

### Identity-specific Keys
Identity specific keys like that inbound messages for a given identity use `{identityKey}` as the hash tag.

## Protocol Buffers
This project uses protocol buffers. Follow the [tutorial](https://developers.google.com/protocol-buffers/docs/gotutorial) to ensure that you have the right tools in place, then run `make` to ensure the protocol buffers are up to date.

## Generating documentation
Documentation is generated using [protoc-gen-doc](https://github.com/pseudomuto/protoc-gen-doc). You can install it with `go get -u github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc`

## Testing
You can run all the tests with `make test-and-cover`.

Tests will also run in GitHub CI. If you install [act](https://github.com/nektos/act) you can also run the whole CI run locally with `COVERALLS_TOKEN=<token> make ci` where `<token>` is the token for uploading to coveralls.io.

The first time you run `make ci` it will take a while because it has to download a bunch of stuff. After that, it should run much faster, unless you change [build_and_test.yml](.github/workflows/build_and_test.yml).

### Smoke Testing
You can run a smoke test against the live environment with `SMOKE_TEST_URL="wss://<host>/api" make smoke-test`, where `<host>` is the host of the tassis server you wish to test.

### curve25519 to ed25519 conversion
Signal uses curve25519 keys for performing elliptic curve diffie helman key exchanges, and it also converts these into ed25519 keys for message signing. The authentication protocol for tassis relies on such signatures, so it needs a way to convert the curve25519 public key into an ed25519 public key. There's no suitable library for this, so we end up relying on code copied verbatim from golang/crypto/master/ed25519/internal/edwards25519/edwards25519.go to help with that conversion.

## logs

Production logs can be found in Logtail at https://logtail.com/
