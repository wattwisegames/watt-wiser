#!/bin/bash

set -euo pipefail

# Ensure checks pass.
./ci-checks.sh

tag="$1"
name="watt-wiser"

function build_release {
    reldir="$name-$tag-$GOOS"
    mkdir "$reldir" || true
    go build -o "$reldir" ./cmd/watt-wiser-sensors/ .
    if [ "$GOOS" = "windows" ]; then
        zip -9 -r "$reldir".zip "$reldir"
    else
        tar -cJvf "$reldir".tar.xz "$reldir"
    fi
    rm -rf "$reldir"
}
export CGO_ENABLED=1
export CC=x86_64-linux-gnu-gcc
export CXX=x86_64-linux-gnu-g++
export GOOS=linux
build_release
export CGO_CFLAGS=-I/usr/x86_64-w64-mingw32/include/
export CC=x86_64-w64-mingw32-gcc
export CXX=x86_64-w64-mingw32-g++
export GOOS=windows
build_release
