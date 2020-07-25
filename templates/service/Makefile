.PHONY: generate

install_truss:
	go get -u -d github.com/metaverse/truss && \
	cd $$GOPATH/src/github.com/metaverse/truss && \
	make dependencies && \
	make

generate:
	truss protos/*.proto --svcout . --verbose