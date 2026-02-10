package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		os.Exit(0)
	}

	var err error
	switch args[0] {
	case "am":
		err = cmdAM(args[1:])
	case "shazam":
		err = cmdShazam(args[1:])
	case "prep":
		err = cmdPrep(args[1:])
	case "diff":
		err = cmdDiff(args[1:])
	case "mbox":
		err = cmdMbox(args[1:])
	case "-version", "--version":
		fmt.Printf("emx-b4 v%s\n", version)
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command %q\n\n", args[0])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fatal("%v", err)
	}
}

func printUsage() {
	fmt.Println(`emx-b4 - Mail-based Git patch workflow tool

Usage:
  emx-b4 <command> [options]

Commands:
  am       Create git-am ready patches from mbox
  shazam   Apply mbox patches to current repository
  prep     Prepare patch series for submission
  diff     Compare patch series versions
  mbox     Parse and display mbox file info

Options:
  --version    Show version
  -h, --help   Show help`)
}
