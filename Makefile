test: bindata.go checks
	go test -v . && printf "\033[1;32;42m               \033[m\033[1;32;48m OK \033[1;32;42m               \033[m\n" || printf "\033[1;31;41m               \033[m\033[1;31;48m EPIC FAIL \033[1;31;41m                 \033[m\n"

build: bindata.go
	go build

bindata.go: deps templates/* templates/*/*
	$(GOPATH)/bin/go-bindata -pkg="pipeline" -o="bindata.go" templates/ templates/jenkins
	go fmt .

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

.PHONY: check bindata.go
