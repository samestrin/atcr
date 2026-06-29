//go:build rust

package main

// active is the compile-time-selected language table. Built with `-tags rust` by
// build.sh to produce rust.wasm.
var active = rustConfig
