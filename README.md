messaging-server [![Travis CI Status](https://travis-ci.org/getlantern/zenodb.svg?branch=master)](https://travis-ci.org/getlantern/zenodb)&nbsp;[![Coverage Status](https://coveralls.io/repos/getlantern/messaging-server/badge.png)](https://coveralls.io/r/getlantern/messaging-server)&nbsp;[![Go Reference](https://pkg.go.dev/badge/github.com/getlantern/messaging-server.svg)](https://pkg.go.dev/github.com/getlantern/messaging-server)&nbsp;[![Sourcegraph](https://sourcegraph.com/github.com/getlantern/messaging-server/-/badge.svg)](https://sourcegraph.com/github.com/getlantern/messaging-server?badge)

See [testsupport/testsupport.go](testsupport/testsupport.go) for a fairly full usage example that illustrates the message exchange pattern.

Comprehensive documentation, including message formats, is in [docs](docs/README.md).

### Protocol Buffers
This project uses protocol buffers. Follow the [tutorial](https://developers.google.com/protocol-buffers/docs/gotutorial) to ensure that you have the right tools in place, then run `make` to ensure the protocol buffers are up to date.

### Generating documentation
Documentation is generated using [protoc-gen-doc](https://github.com/pseudomuto/protoc-gen-doc). You can install it with `go get -u github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc`