// Package main starts Mojian, the MyGo app, and its terminal session
// server; the window itself lives in internal/app.
//
//	go tool mygo dev    the app, rebuilt as the code changes
//	go test ./...       the model, the store and the view, without a window
package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"mojian/internal/app"
	"mojian/internal/session"

	"github.com/egoist/mygo"
)

const usage = `usage: mojian [path]

Open a project folder, or a file inside one:

  mojian .
  mojian ~/git/abc
  mojian ~/git/abc/README.md

  -h, --help  show this help
`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-server" {
		log.SetPrefix("[session] ")
		if err := session.Serve(); err != nil {
			log.Fatal(err)
		}
		return
	}
	startup, err := app.ParseStartup(os.Args[1:], "")
	if err != nil {
		if errors.Is(err, app.ErrHelp) {
			fmt.Print(usage)
			return
		}
		fmt.Fprintf(os.Stderr, "mojian: %v\n\n%s", err, usage)
		os.Exit(2)
	}
	// The session server keeps its socket and log in the app's data
	// directory, apart between the installed app and mygo dev.
	if os.Getenv("MOJIAN_DIR") == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			_ = os.Setenv("MOJIAN_DIR", dir)
		}
	}
	// A launch from a terminal leaves it: re-run in our own session so the
	// shell returns at once and closing the terminal or pressing Ctrl+C no
	// longer ends the window. Errors from here on go to app.log.
	if detachFromTerminal() {
		return
	}
	app.Run(startup)
}
