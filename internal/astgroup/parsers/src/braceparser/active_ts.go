//go:build ts

package main

// active is the compile-time-selected language table. Built with `-tags ts` by
// build.sh to produce ts.wasm (TypeScript/JavaScript).
var active = tsConfig
