# requires github.com/pseudomuto/protoc-gen-doc plugin
model/Messages.pb.go,apidoc.md: model/Messages.proto
	@protoc --go_out=$$GOPATH/src model/Messages.proto --doc_out=docs --doc_opt=docs/README.tmpl,README.md && \
	echo "recompiled protocol buffers"

