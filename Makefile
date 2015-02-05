test: checks
	go test -v .

build: bindata.go
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
	go get golang.org/x/tools/cmd/vet
	go get github.com/golang/lint/golint

example: *.go
	go build -o ppl-example ./example




.PHONY: check
