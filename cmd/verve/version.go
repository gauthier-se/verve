package main

// version is the build version, stamped at release time via
// `-ldflags "-X main.version=..."` (see the Makefile, Dockerfile and
// .goreleaser.yaml). It stays "dev" for local `go build` / `go run`.
var version = "dev"
