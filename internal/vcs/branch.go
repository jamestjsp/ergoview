package vcs

import (
	"os"
	"path/filepath"
	"strings"
)

const shortCommitLength = 7

// Branch reports the checked-out branch of the Git repository containing dir,
// a short commit for a detached HEAD, or "" when dir is not tracked by Git.
func Branch(dir string) string {
	gitDir, ok := findGitDir(dir)
	if !ok {
		return ""
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(head))
	if ref, ok := strings.CutPrefix(value, "ref:"); ok {
		return strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
	}
	if len(value) > shortCommitLength {
		return value[:shortCommitLength]
	}
	return value
}

func findGitDir(dir string) (string, bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, ".git")
		if info, err := os.Stat(candidate); err == nil {
			if info.IsDir() {
				return candidate, true
			}
			// Worktrees and submodules use a .git file pointing at the real Git directory.
			data, err := os.ReadFile(candidate)
			if err != nil {
				return "", false
			}
			target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
			if !ok {
				return "", false
			}
			target = strings.TrimSpace(target)
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}
			return target, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
