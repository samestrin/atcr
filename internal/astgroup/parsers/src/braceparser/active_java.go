//go:build java

package main

// active is the compile-time-selected language table. Built with `-tags java` by
// build.sh to produce java.wasm.
var active = javaConfig
