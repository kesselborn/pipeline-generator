test: bindata.go checks
	go test -v .

build: bindata.go api-docs.html
	go build

bindata.go: deps templates/* templates/*/*
	$(GOPATH)/bin/go-bindata -pkg="pipeline" -o="bindata.go" templates/ templates/jenkins
	# make bindata functions private
	sed -i "" 's/func Asset/func asset/g' $@

checks: deps
	go fmt .
	golint `find . -name "*.go" | grep -v bindata.go` || true
	go vet .

deps:
	go get github.com/jteeuwen/go-bindata/...
	go get code.google.com/p/go.tools/cmd/vet
	go get github.com/golang/lint/golint

api-docs.html: *.go
	godoc -url "pkg/github.com/soundcloud/pipeline-generator/" > $@
	sed -i "" 's#rel="stylesheet" href="#&http://golang.org#g' $@
	sed -i "" 's#type="text/javascript" src="#&http://golang.org#g' $@

example: *.go
	go build -o ppl-example ./example




.PHONY: check
