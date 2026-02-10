package main

import (
	"fmt"
	"os"

	"github.com/emx-mail/cli/pkgs/patchwork"
)

func cmdMbox(args []string) error {
	var mboxFile string

	// Simple positional arg — no flags needed
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printMboxUsage()
			return nil
		}
		if mboxFile == "" {
			mboxFile = arg
		} else {
			return fmt.Errorf("unexpected argument: %s", arg)
		}
	}

	if mboxFile == "" {
		return fmt.Errorf("mbox file is required")
	}

	f, err := os.Open(mboxFile)
	if err != nil {
		return fmt.Errorf("open mbox file: %w", err)
	}
	defer f.Close()

	mb := patchwork.NewMailbox()
	if err := mb.ReadMbox(f); err != nil {
		return fmt.Errorf("parse mbox: %w", err)
	}

	fmt.Printf("Total messages: %d\n", len(mb.Messages))
	fmt.Printf("Versions:       %d\n", len(mb.Series))
	fmt.Printf("Unclassified:   %d\n\n", len(mb.Unknowns))

	for rev, series := range mb.Series {
		fmt.Printf("== Version v%d ==\n", rev)
		if series.CoverLetter != nil {
			fmt.Printf("  Cover: %s\n", series.CoverLetter.Parsed.Subject)
		}
		fmt.Printf("  Patches: %d/%d\n", len(series.Patches), series.Expected)
		for i, p := range series.Patches {
			fmt.Printf("  [%d] %s\n", i+1, p.Parsed.Subject)
			if len(p.BodyParts.Trailers) > 0 {
				for _, t := range p.BodyParts.Trailers {
					fmt.Printf("      %s\n", t.String())
				}
			}
		}
		fmt.Println()
	}

	return nil
}

func printMboxUsage() {
	fmt.Println(`emx-b4 mbox - Show mbox file information

Usage:
  emx-b4 mbox <file>`)
}
