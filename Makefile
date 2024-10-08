# requires github.com/pseudomuto/protoc-gen-doc plugin
model/Messages.pb.go,docs/README.md: model/Messages.proto docs/README.tmpl
	@protoc --go_out=$$GOPATH/src model/Messages.proto --doc_out=docs --doc_opt=docs/README.tmpl,README.md && \
	echo "recompiled protocol buffers"

.PHONY: test-and-cover upload-coverage ci live-test

TEST_PACKAGES = $(shell go list ./...)
COVERED_TEST_PACKAGES = $(shell go list ./... | grep -v identity | grep -v "github.com/getlantern/tassis$$")
COVER_PACKAGES = $(shell echo $(COVERED_TEST_PACKAGES) | tr ' ', ',')

test-and-cover:
	@echo "mode: count" > profile.cov && \
	for pkg in $(TEST_PACKAGES); do \
		echo "Testing $$pkg" && \
		go test -race -v -count 1 -tags="integrationtest" -covermode=atomic -coverprofile=profile_tmp.cov -coverpkg "$(COVER_PACKAGES)" $$pkg || exit 1; \
		tail -n +2 profile_tmp.cov >> profile.cov; \
	done

upload-coverage:
	goveralls -coverprofile=profile.cov -repotoken $$COVERALLS_TOKEN -ignore model/Messages.pb.go

ci:
	@docker stop redis ; \
	docker rm redis ; \
	act -r -s COVERALLS_TOKEN=$$COVERALLS_TOKEN

smoke-test:
	go test -count=1 -tags "smoketest" github.com/getlantern/tassis/web