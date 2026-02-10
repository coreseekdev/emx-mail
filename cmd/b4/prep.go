package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/emx-mail/cli/pkgs/patchwork"
	flag "github.com/spf13/pflag"
)

func cmdPrep(args []string) error {
	if len(args) == 0 {
		printPrepUsage()
		return nil
	}

	switch args[0] {
	case "new":
		return cmdPrepNew(args[1:])
	case "cover":
		return cmdPrepCover(args[1:])
	case "reroll":
		return cmdPrepReroll(args[1:])
	case "patches":
		return cmdPrepPatches(args[1:])
	case "status":
		return cmdPrepStatus(args[1:])
	case "list":
		return cmdPrepList(args[1:])
	default:
		return fmt.Errorf("unknown prep subcommand: %s", args[0])
	}
}

func printPrepUsage() {
	fmt.Println(`emx-b4 prep - Prepare patch series

Usage:
  emx-b4 prep <subcommand> [options]

Subcommands:
  new     Create a new patch branch
  cover   Edit cover letter
  reroll  Bump version number
  patches Generate patch files
  status  Show current status
  list    List all prep branches`)
}

func cmdPrepNew(args []string) error {
	fs := flag.NewFlagSet("prep new", flag.ContinueOnError)
	slug := fs.StringP("name", "n", "", "Branch name")
	baseBranch := fs.StringP("base", "b", "", "Base branch")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *slug == "" && fs.NArg() > 0 {
		s := fs.Arg(0)
		slug = &s
	}

	if *slug == "" {
		return fmt.Errorf("branch name required: emx-b4 prep new <name>")
	}

	git := patchwork.NewGit(".")
	pb, err := patchwork.NewPrepBranch(git, *slug, *baseBranch)
	if err != nil {
		return err
	}

	if err := pb.Create(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Created patch branch: %s\n", pb.BranchName())
	return nil
}

func cmdPrepCover(args []string) error {
	fs := flag.NewFlagSet("prep cover", flag.ContinueOnError)
	subject := fs.StringP("subject", "s", "", "Cover subject")
	body := fs.StringP("body", "b", "", "Cover body")

	if err := fs.Parse(args); err != nil {
		return err
	}

	git := patchwork.NewGit(".")
	pb, err := patchwork.LoadPrepBranch(git)
	if err != nil {
		return err
	}

	if *subject == "" {
		*subject = pb.CoverSubject
	}

	if err := pb.SaveCover(*subject, *body); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Cover letter saved\n")
	return nil
}

func cmdPrepReroll(args []string) error {
	git := patchwork.NewGit(".")
	pb, err := patchwork.LoadPrepBranch(git)
	if err != nil {
		return err
	}

	oldRev := pb.Revision
	if err := pb.Reroll(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Version bumped: v%d -> v%d\n", oldRev, pb.Revision)
	return nil
}

func cmdPrepPatches(args []string) error {
	fs := flag.NewFlagSet("prep patches", flag.ContinueOnError)
	outputDir := fs.StringP("output", "o", "", "Output directory")

	if err := fs.Parse(args); err != nil {
		return err
	}

	git := patchwork.NewGit(".")
	pb, err := patchwork.LoadPrepBranch(git)
	if err != nil {
		return err
	}

	paths, err := pb.GetPatches(*outputDir)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Generated %d patches:\n", len(paths))
	for _, p := range paths {
		fmt.Println(p)
	}
	return nil
}

func cmdPrepStatus(args []string) error {
	git := patchwork.NewGit(".")
	pb, err := patchwork.LoadPrepBranch(git)
	if err != nil {
		return err
	}

	fmt.Printf("Branch:  %s\n", pb.BranchName())
	fmt.Printf("Version: v%d\n", pb.Revision)
	fmt.Printf("Base:    %s\n", pb.BaseBranch)
	if pb.ChangeID != "" {
		fmt.Printf("Change-ID: %s\n", pb.ChangeID)
	}
	if pb.CoverSubject != "" {
		fmt.Printf("Cover subject: %s\n", pb.CoverSubject)
	}

	commits, err := pb.EnumerateCommits()
	if err == nil && len(commits) > 0 {
		fmt.Printf("\nCommits (%d):\n", len(commits))
		for i, c := range commits {
			// Only show subject (trim hash if present)
			line := c
			if idx := strings.Index(c, " "); idx > 0 {
				line = c[idx+1:]
			}
			fmt.Printf("  %d. %s\n", i+1, line)
		}
	}

	stat, err := pb.DiffStat()
	if err == nil && stat != "" {
		fmt.Printf("\nDiffstat:\n%s", stat)
	}

	return nil
}

func cmdPrepList(args []string) error {
	git := patchwork.NewGit(".")
	branches, err := patchwork.ListPrepBranches(git)
	if err != nil {
		return err
	}

	if len(branches) == 0 {
		fmt.Println("No prep branches found")
		return nil
	}

	for _, b := range branches {
		fmt.Println(b)
	}
	return nil
}
