package vcs

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBranchReadsCheckedOutBranchFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/feature/sort\n")
	nested := filepath.Join(root, "sub", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Branch(nested); got != "feature/sort" {
		t.Fatalf("Branch() = %q, want feature/sort", got)
	}
}

func TestBranchFollowsWorktreeGitFile(t *testing.T) {
	base := t.TempDir()
	gitDir := filepath.Join(base, "main", ".git", "worktrees", "wt-1")
	writeFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/wt-1\n")
	worktree := filepath.Join(base, "wt-1")
	writeFile(t, filepath.Join(worktree, ".git"), "gitdir: "+gitDir+"\n")
	if got := Branch(worktree); got != "wt-1" {
		t.Fatalf("Branch() = %q, want wt-1", got)
	}
}

func TestBranchFollowsRelativeGitFile(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, ".git", "modules", "child", "HEAD"), "ref: refs/heads/main\n")
	child := filepath.Join(base, "child")
	writeFile(t, filepath.Join(child, ".git"), "gitdir: ../.git/modules/child\n")
	if got := Branch(child); got != "main" {
		t.Fatalf("Branch() = %q, want main", got)
	}
}

func TestBranchShortensDetachedHead(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git", "HEAD"), "62ed700a1b2c3d4e5f60718293a4b5c6d7e8f901\n")
	if got := Branch(root); got != "62ed700" {
		t.Fatalf("Branch() = %q, want 62ed700", got)
	}
}

func TestBranchIsEmptyOutsideGit(t *testing.T) {
	if got := Branch(t.TempDir()); got != "" {
		t.Fatalf("Branch() = %q, want empty", got)
	}
}
