package main

import "fmt"

// version is set at build time via ldflags:
//
//	go build -ldflags "-X main.version=v1.0.0" ./cmd/opcode/
var version = "dev"

func printVersion() {
	fmt.Println(version)
}
