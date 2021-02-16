# requires github.com/pseudomuto/protoc-gen-doc plugin
model/Messages.pb.go,docs/README.md: model/Messages.proto docs/README.tmpl
	@protoc --go_out=$$GOPATH/src model/Messages.proto --doc_out=docs --doc_opt=docs/README.tmpl,README.md && \
	echo "recompiled protocol buffers"

.PHONY: test-and-cover upload-coverage

test-and-cover:
	@echo "mode: count" > profile.cov && \
	TP=$$(go list ./...) && \
	CP=$$(echo $$TP | tr ' ', ',') && \
	set -x && \
	for pkg in $$TP; do \
		GO111MODULE=on go test -race -v -tags="headless" -covermode=atomic -coverprofile=profile_tmp.cov -coverpkg "$$CP" $$pkg || exit 1; \
		tail -n +2 profile_tmp.cov >> profile.cov; \
	done

upload-coverage:
	goveralls -coverprofile=profile.cov -repotoken $$COVERALLS_TOKEN -ignore model/Messages.pb.go