//go:build darwin || linux || windows

// Package main is Mojian's session and project server, the binary Mojian
// uploads to an SSH host; `-stop` ends a running one and `stdio` bridges
// a connection's stdio to it.
package main

import (
	"log"
	"os"

	"mojian/internal/session"
)

func main() {
	log.SetPrefix("[mojian-server] ")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-stop":
			if err := session.Stop(); err != nil {
				log.Fatal(err)
			}
			return
		case "stdio":
			if err := session.Bridge(); err != nil {
				log.Fatal(err)
			}
			return
		case "-server":
			// session.Spawn starts a detached server with the flag the app
			// binary uses, so this binary takes it too and serves below.
		default:
			log.Fatalf("unknown argument %q", os.Args[1])
		}
	}
	if err := session.Serve(); err != nil {
		log.Fatal(err)
	}
}
