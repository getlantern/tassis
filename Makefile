model/Messages.pb.go,apidoc.md: model/Messages.proto
	@protoc --go_out=$$GOPATH/src model/Messages.proto --doc_out=. --doc_opt=markdown,apidoc.md && \
	echo "recompiled protocol buffers"

