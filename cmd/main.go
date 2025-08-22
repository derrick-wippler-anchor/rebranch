package main

import (
	"flag"
	"fmt"
	"os"

	"rebranch"
)

const version = "1.0.0"

func main() {
	var showHelp bool
	var showVersion bool

	flag.BoolVar(&showHelp, "help", false, "Show help information")
	flag.BoolVar(&showHelp, "h", false, "Show help information")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&showVersion, "v", false, "Show version information")

	// Parse flags but don't exit on error, we'll handle it ourselves
	flag.CommandLine.Init(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(os.Stderr)
	
	err := flag.CommandLine.Parse(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			showHelp = true
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	if showVersion {
		fmt.Printf("rebranch version %s\n", version)
		return
	}

	if showHelp {
		printHelp()
		return
	}

	// Get remaining arguments after flag parsing
	args := flag.Args()

	if err := rebranch.RunCmd(args, rebranch.Options{}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`rebranch - Interactive Git branch rebasing tool

USAGE:
    rebranch <base-branch>    Start interactive rebranch onto base-branch
    rebranch --continue       Continue after resolving conflicts
    rebranch --done           Complete rebranch and replace original branch
    rebranch --abort          Cancel rebranch and cleanup

OPTIONS:
    -h, --help               Show this help message
    -v, --version            Show version information

DESCRIPTION:
    rebranch allows you to interactively cherry-pick commits from your current
    branch onto a new base, with conflict resolution support and safe rollback.

WORKFLOW:
    1. Start: rebranch <base-branch>
       - Shows list of commits to be applied
       - Opens editor for interactive selection (pick/drop)
       - Creates temporary branch and begins cherry-picking

    2. Resolve conflicts (if any):
       - Edit conflicted files
       - Stage resolved files: git add <files>
       - Continue: rebranch --continue

    3. Review and finish:
       - Inspect the rebranched commits
       - Complete: rebranch --done (replaces original branch)
       - Or cancel: rebranch --abort (reverts to original state)

INTERACTIVE FILE FORMAT:
    pick abc1234 First commit    # Apply this commit
    p    def5678 Second commit   # Apply (abbreviation)
    drop ghi9012 Third commit    # Skip this commit  
    d    jkl3456 Fourth commit   # Skip (abbreviation)

EXAMPLES:
    rebranch main               # Rebranch current branch onto main
    rebranch --continue         # Resume after conflict resolution
    rebranch --done             # Finish successful rebranch
    rebranch --abort            # Cancel and cleanup

ENVIRONMENT:
    EDITOR                      Editor for interactive commit selection
                               (defaults to 'vi' if not set)
`)
}