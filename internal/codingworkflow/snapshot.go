package codingworkflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// git disables hooks/fsmonitor/replace refs and never interpolates shell text.
func git(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"}
	var out, errs logBuffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s failed: %w", args[0], err)
	}
	if out.truncated || errs.truncated {
		return "", errors.New("git output exceeds limit")
	}
	return out.String(), nil
}

func repoName(origin string) string {
	origin = strings.TrimSpace(origin)
	origin = strings.TrimSuffix(origin, ".git")
	for _, prefix := range []string{"https://github.com/", "git@github.com:", "ssh://git@github.com/"} {
		if strings.HasPrefix(origin, prefix) {
			return strings.TrimPrefix(origin, prefix)
		}
	}
	return ""
}

// TakeSnapshot binds Git HEAD and base..HEAD patch plus ALL non-.git files,
// including ignored/untracked files. Evidence/build outputs belong outside root.
// Symlinks, nested repositories, special files, and oversized trees fail closed.
func TakeSnapshot(ctx context.Context, root string, c Contract) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := ValidateContract(c); err != nil {
		return Snapshot{}, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return Snapshot{}, err
	}
	top, err := git(ctx, abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return Snapshot{}, err
	}
	top, err = filepath.EvalSymlinks(strings.TrimSpace(top))
	if err != nil || top != abs {
		return Snapshot{}, errors.New("repo must be the Git working tree root")
	}
	origin, err := git(ctx, abs, "config", "--get", "remote.origin.url")
	if err != nil || repoName(origin) != c.Repository {
		return Snapshot{}, errors.New("repository identity mismatch")
	}
	if _, err := git(ctx, abs, "merge-base", "--is-ancestor", c.BaseSHA, "HEAD"); err != nil {
		return Snapshot{}, errors.New("base SHA is not an ancestor of HEAD")
	}
	head, err := git(ctx, abs, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Snapshot{}, err
	}
	tree, err := git(ctx, abs, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return Snapshot{}, err
	}
	patch, err := git(ctx, abs, "diff", "--no-ext-diff", "--no-textconv", "--binary", c.BaseSHA, "HEAD", "--")
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{HeadSHA: strings.TrimSpace(head), TreeSHA: strings.TrimSpace(tree), PatchSHA256: Hash([]byte(patch))}
	if !shaPattern.MatchString(s.HeadSHA) || !shaPattern.MatchString(s.TreeSHA) {
		return Snapshot{}, errors.New("unsupported Git object format")
	}
	var total int64
	err = filepath.WalkDir(abs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == ".git" {
			return errors.New("nested Git repositories are not supported")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular source file: %s", rel)
		}
		total += info.Size()
		if len(s.Files) >= 10000 || info.Size() > 8<<20 || total > 64<<20 {
			return errors.New("source snapshot exceeds bounds")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, (8<<20)+1))
		closeErr := f.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if int64(len(data)) != info.Size() {
			return errors.New("source changed during snapshot")
		}
		s.Files = append(s.Files, File{filepath.ToSlash(rel), uint32(info.Mode().Perm()), Hash(data)})
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	status, err := git(ctx, abs, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return Snapshot{}, err
	}
	s.StatusSHA256 = Hash([]byte(status))
	s.WorktreeSHA256 = hashJSON(s.Files)
	// Guard against concurrent commit switches while reading file bytes.
	headAfter, err := git(ctx, abs, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(headAfter) != s.HeadSHA {
		return Snapshot{}, errors.New("HEAD changed during snapshot")
	}
	return s, nil
}
