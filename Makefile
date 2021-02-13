model/Messages.pb.go: model/Messages.proto
	@protoc --go_out=$$GOPATH/src model/Messages.proto && \
	echo "recompiled protocol buffers"