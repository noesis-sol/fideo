#!/bin/sh
# scripts/check-fast.sh: orchestra's merge check, run on every ticket rebased onto work merged while it ran.
# orchestra init wrote it; it is the project's to edit. Each suite is one command, run from the
# repository's root, and finds its own test files: a new test in an existing suite needs no edit here.
set -e
cd "$(dirname "$0")/.."

# gofmt check, from .github/workflows/ci.yml:37
printf '%s\n' 'SUITE: gofmt check'
test -z "$(gofmt -l .)"

# go vet, from .github/workflows/ci.yml:45
printf '%s\n' 'SUITE: go vet'
go vet ./...

# build, from .github/workflows/ci.yml:48
printf '%s\n' 'SUITE: build'
go build ./cmd/fideo

# unit tests, from .github/workflows/ci.yml:54
printf '%s\n' 'SUITE: unit tests'
go test ./...
