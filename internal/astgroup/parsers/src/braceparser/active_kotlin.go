//go:build kotlin

package main

// active is the compile-time-selected language table. Built with `-tags kotlin` by
// build.sh to produce kotlin.wasm.
var active = kotlinConfig
