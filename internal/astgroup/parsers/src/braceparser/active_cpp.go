//go:build cpp

package main

// active is the compile-time-selected language table. Built with `-tags cpp` by
// build.sh to produce cpp.wasm (C and C++).
var active = cppConfig
