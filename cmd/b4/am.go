package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/emx-mail/cli/pkgs/patchwork"
	flag "github.com/spf13/pflag"
)

func cmdAM(args []string) error {
	fs := flag.NewFlagSet("am", flag.ContinueOnError)
	mboxFile := fs.StringP("mbox", "m", "", "Input mbox file (default: stdin)")
	output := fs.StringP("output", "o", "", "Output file (default: stdout)")
	revision := fs.IntP("revision", "v", 0, "Select patch revision (default: latest)")
	threeWay := fs.BoolP("3way", "3", false, "Enable 3-way merge")
	addLink := fs.Bool("add-link", false, "Add Link: trailer")
	linkPrefix := fs.String("link-prefix", "", "Link URL prefix")
	addMsgID := fs.Bool("add-message-id", false, "Add Message-Id trailer")
	coverTrails := fs.Bool("apply-cover-trailers", false, "Apply cover letter trailers to all patches")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Remaining positional arg is mbox file
	if *mboxFile == "" && fs.NArg() > 0 {
		*mboxFile = fs.Arg(0)
	}

	_ = *threeWay // used in shazam

	// Read input
	var reader io.Reader
	if *mboxFile == "" || *mboxFile == "-" {
		reader = os.Stdin
	} else {
		f, err := os.Open(*mboxFile)
		if err != nil {
			return fmt.Errorf("open mbox file: %w", err)
		}
		defer f.Close()
		reader = f
	}

	// Parse mbox
	mb := patchwork.NewMailbox()
	if err := mb.ReadMbox(reader); err != nil {
		return fmt.Errorf("parse mbox: %w", err)
	}

	series := mb.GetSeries(*revision)
	if series == nil {
		return fmt.Errorf("patch series not found (revision %d)", *revision)
	}

	if !series.Complete {
		fmt.Fprintf(os.Stderr, "Warning: incomplete patch series (expected %d, found %d)\n",
			series.Expected, len(series.Patches))
	}

	opts := patchwork.AMReadyOptions{
		AddLink:            *addLink,
		LinkPrefix:         *linkPrefix,
		AddMessageID:       *addMsgID,
		ApplyCoverTrailers: *coverTrails,
	}

	data, err := series.GetAMReady(opts)
	if err != nil {
		return fmt.Errorf("generate AM patches: %w", err)
	}

	if *output == "" || *output == "-" {
		os.Stdout.Write(data)
	} else {
		if err := os.WriteFile(*output, data, 0644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Saved to %s (%d patches)\n", *output, len(series.Patches))
	}

	return nil
}

func cmdShazam(args []string) error {
	fs := flag.NewFlagSet("shazam", flag.ContinueOnError)
	mboxFile := fs.StringP("mbox", "m", "", "Input mbox file (default: stdin)")
	revision := fs.IntP("revision", "v", 0, "Select patch revision (default: latest)")
	threeWay := fs.BoolP("3way", "3", false, "Enable 3-way merge")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *mboxFile == "" && fs.NArg() > 0 {
		*mboxFile = fs.Arg(0)
	}

	// Read input
	var reader io.Reader
	if *mboxFile == "" || *mboxFile == "-" {
		reader = os.Stdin
	} else {
		f, err := os.Open(*mboxFile)
		if err != nil {
			return fmt.Errorf("open mbox file: %w", err)
		}
		defer f.Close()
		reader = f
	}

	mb := patchwork.NewMailbox()
	if err := mb.ReadMbox(reader); err != nil {
		return fmt.Errorf("parse mbox: %w", err)
	}

	series := mb.GetSeries(*revision)
	if series == nil {
		return fmt.Errorf("patch series not found (revision %d)", *revision)
	}

	opts := patchwork.AMReadyOptions{
		ApplyCoverTrailers: true,
	}
	data, err := series.GetAMReady(opts)
	if err != nil {
		return fmt.Errorf("generate AM patches: %w", err)
	}

	git := patchwork.NewGit(".")
	if !git.IsRepo() {
		return fmt.Errorf("current directory is not a git repository")
	}

	fmt.Fprintf(os.Stderr, "Applying %d patches...\n", len(series.Patches))

	if err := git.AMFromBytes(data, *threeWay); err != nil {
		return fmt.Errorf("apply patches failed: %w\nHint: use 'git am --abort' to cancel", err)
	}

	fmt.Fprintf(os.Stderr, "Successfully applied %d patches\n", len(series.Patches))
	return nil
}

func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	mboxFile := fs.StringP("mbox", "m", "", "Input mbox file")
	rangeStr := fs.StringP("range", "r", "", "Version range (e.g., 1..2)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *mboxFile == "" && fs.NArg() > 0 {
		*mboxFile = fs.Arg(0)
	}

	if *mboxFile == "" {
		return fmt.Errorf("mbox file is required")
	}

	var rev1, rev2 int
	if *rangeStr != "" {
		parts := strings.SplitN(*rangeStr, "..", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid range format, expected N..M (e.g., 1..2)")
		}
		var err error
		rev1, err = strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid revision number %q: %w", parts[0], err)
		}
		rev2, err = strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid revision number %q: %w", parts[1], err)
		}
	}

	f, err := os.Open(*mboxFile)
	if err != nil {
		return fmt.Errorf("open mbox file: %w", err)
	}
	defer f.Close()

	mb := patchwork.NewMailbox()
	if err := mb.ReadMbox(f); err != nil {
		return fmt.Errorf("parse mbox: %w", err)
	}

	series1 := mb.GetSeries(rev1)
	series2 := mb.GetSeries(rev2)

	if series1 == nil {
		return fmt.Errorf("version v%d not found", rev1)
	}
	if series2 == nil {
		return fmt.Errorf("version v%d not found", rev2)
	}

	fmt.Printf("== Comparing v%d (%d patches) vs v%d (%d patches) ==\n\n",
		series1.Revision, len(series1.Patches),
		series2.Revision, len(series2.Patches))

	maxPatches := len(series1.Patches)
	if len(series2.Patches) > maxPatches {
		maxPatches = len(series2.Patches)
	}

	for i := 0; i < maxPatches; i++ {
		var subj1, subj2 string
		if i < len(series1.Patches) {
			subj1 = series1.Patches[i].Parsed.Subject
		} else {
			subj1 = "(missing)"
		}
		if i < len(series2.Patches) {
			subj2 = series2.Patches[i].Parsed.Subject
		} else {
			subj2 = "(missing)"
		}

		if subj1 != subj2 {
			fmt.Printf("Patch %d: changed\n  v%d: %s\n  v%d: %s\n\n",
				i+1, series1.Revision, subj1, series2.Revision, subj2)
		} else {
			fmt.Printf("Patch %d: same - %s\n", i+1, subj1)
		}
	}

	return nil
}
