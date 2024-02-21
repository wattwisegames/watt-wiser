#!/bin/bash

set -euo pipefail

function run_checks {
    echo checking $GOOS
    go test ./...
    staticcheck ./...
}
export CGO_ENABLED=1
export CC=x86_64-linux-gnu-gcc
export CXX=x86_64-linux-gnu-g++
export GOOS=linux
run_checks
export CGO_CFLAGS=-I/usr/x86_64-w64-mingw32/include/
export CC=x86_64-w64-mingw32-gcc
export CXX=x86_64-w64-mingw32-g++
export GOOS=windows
run_checks
