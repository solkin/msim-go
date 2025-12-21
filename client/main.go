package main

import (
	"flag"
	"fmt"
	"os"

	"msim-client/ui"
)

func main() {
	serverAddr := flag.String("server", "localhost:3215", "mSIM server address (host:port)")
	flag.Parse()

	app := ui.NewApp(*serverAddr)
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

