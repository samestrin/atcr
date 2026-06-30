//go:build csharp

package main

// active is the compile-time-selected language table. Built with `-tags csharp` by
// build.sh to produce csharp.wasm.
var active = csharpConfig
