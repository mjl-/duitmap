export CGO_ENABLED=0
export GOFLAGS=-mod=vendor
export GOPROXY=off

build:
	go build
	go vet
	golint

install:
	go install

clean:
	go clean

fmt:
	go fmt ./...
