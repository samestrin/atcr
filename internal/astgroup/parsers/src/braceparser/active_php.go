//go:build php

package main

// active is the compile-time-selected language table. Built with `-tags php` by
// build.sh to produce php.wasm.
var active = phpConfig
