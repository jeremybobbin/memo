#!/bin/make -f
build: *.go
	go build

test: *.go
	go test
