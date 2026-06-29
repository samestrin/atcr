//go:build bash

package main

// active is the compile-time-selected language table. Built with `-tags bash` by
// build.sh to produce bash.wasm.
var active = bashConfig
