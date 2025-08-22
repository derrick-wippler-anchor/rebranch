# rebranch

Interactive Git branch rebasing tool with conflict resolution and safe rollback.

## Why?

Using stacked branches creates a long history of commits from dependant stacked
branches. Once dependant branches are merged, git still believes the stacked
branch contains the commits from the source stacked branch as mergify rewrites
those commits during merge.

This tool allows you to quickly re-write the history of your dependant stacked
branch from the new master, while removing any commits that have already been
merged.

## Overview

`rebranch` allows you to interactively cherry-pick commits from your current
branch onto a new base, with built-in conflict resolution and the ability to
safely rollback changes. Think of it as an interactive `git rebase` with better
safety controls.

## Features

- 🎯 **Interactive commit selection** - Choose which commits to apply
- 🔄 **Safe rollback** - Abort at any time to return to original state  
- ⚡ **Conflict resolution** - Built-in workflow for handling merge conflicts
- 📝 **Abbreviations** - Use `p` for pick, `d` for drop (like `git rebase -i`)
- 🔍 **State management** - Resume operations after conflicts or interruptions
- ✅ **Comprehensive validation** - Prevents unsafe operations

## Installation

```bash
go install github.com/derrick-wippler-anchor/rebranch@latest
```

## Quick Start

```bash
# 1. Switch to your feature branch
git checkout feature-branch

# 2. Start interactive rebranch
rebranch main

# 3. Edit the commit list (pick/drop commits)
# 4. Handle any conflicts if they occur
# 5. Complete the rebranch
rebranch --done
```

## Usage

### Commands

| Command | Description |
|---------|-------------|
| `rebranch <base-branch>` | Start interactive rebranch onto base-branch |
| `rebranch --continue` | Continue after resolving conflicts |
| `rebranch --done` | Complete rebranch and replace original branch |
| `rebranch --abort` | Cancel rebranch and cleanup |
| `rebranch --help` | Show help information |
| `rebranch --version` | Show version information |

### Interactive File Format

When you run `rebranch <base-branch>`, an editor opens with your commits:

```
# Interactive rebranch - Edit the list of commits to apply
# Commands:
#  pick, p = apply this commit
#  drop, d = skip this commit

pick abc1234 Add user authentication
p    def5678 Fix login validation  
drop ghi9012 Debug logging (temporary)
d    jkl3456 Work in progress commit
```

**Actions:**
- `pick` or `p` - Apply this commit to the new branch
- `drop` or `d` - Skip this commit (won't be applied)

## Workflow Examples

### Basic Rebranch

```bash
# Current branch: feature-auth (3 commits ahead of main)
git checkout feature-auth

# Start rebranch onto main
rebranch main
# Editor opens showing 3 commits, all marked as "pick"
# Save and close editor

# If no conflicts:
rebranch --done
# feature-auth now contains rebranched commits on main
```

### Selective Commit Application

```bash
git checkout feature-cleanup

rebranch main
# In editor, change some commits from "pick" to "drop":
# pick abc1234 Important feature
# drop def5678 Temporary debug code  
# pick ghi9012 Bug fix
# Save and close

rebranch --done
# Only the "pick" commits are applied
```

### Handling Conflicts

```bash
git checkout feature-branch
rebranch main

# If conflicts occur:
# Error: conflict during cherry-pick of abc1234 (Add feature)
# 
# To resolve:
#   1. Edit conflicted files to resolve conflicts
#   2. Stage resolved files: git add <files>
#   3. Continue rebranch: rebranch --continue
#   4. Or abort rebranch: rebranch --abort

# Resolve conflicts
vim conflicted-file.js
git add conflicted-file.js

# Continue rebranch
rebranch --continue

# Complete when finished
rebranch --done
```

### Aborting Operation

```bash
rebranch main
# ... conflicts occur or you change your mind

rebranch --abort
# Returns to original branch state
# Temporary branch is deleted
# All changes are reverted
```

## Advanced Usage

### Environment Variables

- `EDITOR` - Editor for interactive commit selection (defaults to `vi`)

```bash
export EDITOR=nano
rebranch main  # Uses nano instead of vi
```

### Checking Operation Status

```bash
# If you forget what operation is in progress:
rebranch --continue
# Shows current status and available actions

git status
# Shows current Git state and conflicts (if any)
```

### Integration with Git Workflows

```bash
# Typical feature branch workflow:
git checkout -b feature-new-ui
# ... make commits ...

# Rebranch onto updated main:
git fetch origin
rebranch origin/main

# Review rebranched commits:
git log --oneline

# Complete rebranch:
rebranch --done

# Push rebranched feature:
git push origin feature-new-ui
```

## Safety Features

### Pre-flight Checks
- ✅ Repository is valid Git repository
- ✅ Working directory is clean  
- ✅ No ongoing Git operations (merge, rebase, etc.)
- ✅ Base branch exists
- ✅ Current branch differs from base branch
- ✅ No existing rebranch operation in progress

### State Management
- Operation state saved in `.git/REBRANCH_STATE`
- Can resume after conflicts, interruptions, or system restart
- Safe cleanup on abort (temporary branches deleted)

### Rollback Protection
- Original branch preserved until `rebranch --done`
- Temporary branch used for all operations
- Easy abort returns to exact original state

## Error Messages

`rebranch` provides detailed, actionable error messages:

```bash
$ rebranch nonexistent-branch
Error: base branch 'nonexistent-branch' does not exist

Suggestions:
  • Check branch name spelling
  • Run 'git branch -a' to see all available branches  
  • Create the branch: git checkout -b nonexistent-branch
```

## Comparison with Git Rebase

| Feature | `git rebase -i` | `rebranch` |
|---------|-----------------|------------|
| Interactive selection | ✅ | ✅ |
| Conflict resolution | ✅ | ✅ |
| Safe abort | ✅ | ✅ |
| Rollback protection | ⚠️ | ✅ |
| Resume after reboot | ⚠️ | ✅ |
| Actionable errors | ❌ | ✅ |
| Pre-flight validation | ❌ | ✅ |

## Troubleshooting

### Common Issues

**"Working directory is not clean"**
```bash
# Commit or stash changes first
git add .
git commit -m "WIP: save changes"
# or
git stash
```

**"Operation already in progress"**
```bash
# Check what operation is running
rebranch --continue  # or --done or --abort
```

**"No commits to rebranch"**
```bash
# Branch is already up-to-date with base
git log --oneline main..feature-branch
```

**Conflicts during cherry-pick**
```bash
# 1. Edit conflicted files
# 2. Stage resolved files
git add resolved-file.js
# 3. Continue
rebranch --continue
```

### Recovery

If something goes wrong, you can always safely abort:
```bash
rebranch --abort
# Returns to exact original state
```

The original branch is never modified until you run `rebranch --done`.

## Development

### Building

```bash
go build -o rebranch ./cmd
```

### Testing

```bash
go test -v ./...
```
