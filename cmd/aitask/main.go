package main

import (
	"fmt"
	"os"

	"github.com/iwen-conf/aitask-cli/internal/cli"
)

var version = "dev"

func main() {
	app := cli.NewApp(version)
	if err := app.Execute(os.Args[1:]); err != nil {
		cli.WriteCommandError(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "hint: use --help for command usage")
		os.Exit(1)
	}
}
