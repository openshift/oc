package release

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

// git is a wrapper to invoke git safely, similar to
// github.com/openshift/library-go/pkg/git but giving access to lower level
// calls. Consider improving pkg/git in the future.
type git struct {
	path string
}

type gitInterface interface {
	exec(command ...string) (string, error)
}

var noSuchRepo = errors.New("location is not a git repo")

func (g *git) exec(command ...string) (string, error) {
	buf := &bytes.Buffer{}
	bufErr := &bytes.Buffer{}
	cmd := exec.Command("git", command...)
	cmd.Dir = g.path
	cmd.Stdout = buf
	cmd.Stderr = bufErr
	klog.V(5).Infof("Executing git: %v\n", cmd.Args)
	err := cmd.Run()
	if err != nil {
		return bufErr.String(), err
	}
	return buf.String(), nil
}

func (g *git) streamExec(out, errOut io.Writer, command ...string) error {
	cmd := exec.Command("git", command...)
	cmd.Dir = g.path
	cmd.Stdout = out
	cmd.Stderr = errOut
	return cmd.Run()
}

func (g *git) ChangeContext(path string) (*git, error) {
	location := &git{path: path}
	if errOut, err := location.exec("rev-parse", "--git-dir"); err != nil {
		if strings.Contains(strings.ToLower(errOut), "not a git repository") {
			return location, noSuchRepo
		}
		return location, err
	}
	return location, nil
}

func (g *git) Clone(repository string, out, errOut io.Writer) error {
	cmd := exec.Command("git", "clone", "--filter=blob:none", "--bare", "--origin="+remoteNameForRepo(repository), repository, g.path)
	cmd.Stdout = out
	cmd.Stderr = errOut
	return cmd.Run()
}

func (g *git) parent() *git {
	return &git{path: filepath.Dir(g.path)}
}

func (g *git) basename() string {
	return filepath.Base(g.path)
}

func (g *git) VerifyCommit(repo, commit string) (bool, error) {
	_, err := g.exec("rev-parse", commit)
	if err == nil {
		return true, nil
	}

	// try to fetch by URL
	klog.V(4).Infof("failed to find commit, fetching: %v", err)
	if err := ensureFetchedRemoteForRepo(g, repo); err != nil {
		return false, err
	}
	_, err = g.exec("rev-parse", commit)
	return err == nil, nil
}

func (g *git) CheckoutCommit(repo, commit string, out, errOut io.Writer) error {
	// to reduce size requirements, clones are normally bare; to checkout a git commit, we must convert it to a normal
	// git directory
	if err := g.ensureFullClone(out, errOut); err != nil {
		return err
	}

	_, err := g.exec("checkout", commit)
	if err == nil {
		return nil
	}

	// try to fetch by URL
	klog.V(4).Infof("failed to checkout: %v", err)
	if err := ensureFetchedRemoteForRepo(g, repo); err == nil {
		if _, err := g.exec("checkout", commit); err == nil {
			return nil
		}
	} else {
		klog.V(4).Infof("failed to fetch: %v", err)
	}

	return fmt.Errorf("could not locate commit %s", commit)
}

func (g *git) ensureFullClone(out, errOut io.Writer) error {
	isBare, err := g.exec("config", "core.bare")
	if err != nil {
		return err
	}
	if isBare == "false\n" {
		return nil
	}
	// move all files to a `.git` subdirectory
	if err := os.Mkdir(filepath.Join(g.path, ".git"), 0755); err != nil {
		return fmt.Errorf("Failed to create .git subdirectory: %v", err)
	}
	if err := filepath.WalkDir(g.path, func(path string, d fs.DirEntry, err error) error {
		if path == g.path {
			return nil
		}
		if d.Name() == ".git" {
			return fs.SkipDir
		}
		if err := os.Rename(filepath.Join(path), filepath.Join(g.path, ".git", d.Name())); err != nil {
			return fmt.Errorf("Failed to move bare git contents to .git subdirectory: %v", err)
		}
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	}); err != nil {
		return err
	}
	if out, err := g.exec("config", "core.bare", "false"); err != nil {
		return fmt.Errorf("Failed to mark git directory as not bare: %s", out)
	}
	out.Write([]byte(fmt.Sprintf("Converting %s to a non-bare git repo", g.path)))
	cmd := exec.Command("git", "reset", "--hard")
	cmd.Dir = g.path
	cmd.Stdout = out
	cmd.Stderr = errOut
	return cmd.Run()
}

var reMatch = regexp.MustCompile(`^([a-zA-Z0-9\-\_]+)@([^:]+):(.+)$`)

func sourceLocationAsURL(location string) (*url.URL, error) {
	if matches := reMatch.FindStringSubmatch(location); matches != nil {
		return &url.URL{Scheme: "git", User: url.UserPassword(matches[1], ""), Host: matches[2], Path: matches[3]}, nil
	}
	return url.Parse(location)
}

func sourceLocationAsRelativePath(dir, location string) (string, error) {
	u, err := sourceLocationAsURL(location)
	if err != nil {
		return "", err
	}
	gitPath := u.Path
	if strings.HasSuffix(gitPath, ".git") {
		gitPath = strings.TrimSuffix(gitPath, ".git")
	}
	gitPath = path.Clean(gitPath)
	basePath := filepath.Join(dir, u.Host, filepath.FromSlash(gitPath))
	return basePath, nil
}

type MergeCommit struct {
	CommitDate time.Time

	Commit        string
	ParentCommits []string

	PullRequest int
	Refs        RefList

	Subject string
}

func gitOutputToError(err error, out string) error {
	out = strings.TrimSpace(out)
	if strings.HasPrefix(out, "fatal: ") {
		out = strings.TrimPrefix(out, "fatal: ")
	}
	if len(out) == 0 {
		return err
	}
	return errors.New(out)
}

var (
	squashRePR = regexp.MustCompile(`[(]#(\d+)[)]`)
	rePR       = regexp.MustCompile(`^Merge pull request #(\d+) from`)
	rePrefix   = regexp.MustCompile(`^(\[[\w\.\-]+\]\s*)+`)
)

func mergeLogForRepo(g gitInterface, repo string, from, to string) ([]MergeCommit, int, error) {
	if from == to {
		return nil, 0, nil
	}

	args := []string{"log", "--merges", "--topo-order", "--first-parent", "-z", "--pretty=format:%H %P%x1E%ct%x1E%s%x1E%b", fmt.Sprintf("%s..%s", from, to)}
	out, err := g.exec(args...)
	if err != nil {
		// retry once if there's a chance we haven't fetched the latest commits
		if !strings.Contains(out, "Invalid revision range") {
			return nil, 0, gitOutputToError(err, out)
		}
		if _, err := g.exec("fetch", "--all"); err != nil {
			return nil, 0, gitOutputToError(err, out)
		}
		if _, err := g.exec("cat-file", "-e", from+"^{commit}"); err != nil {
			return nil, 0, fmt.Errorf("from commit %s does not exist", from)
		}
		if _, err := g.exec("cat-file", "-e", to+"^{commit}"); err != nil {
			return nil, 0, fmt.Errorf("to commit %s does not exist", to)
		}
		out, err = g.exec(args...)
		if err != nil {
			return nil, 0, gitOutputToError(err, out)
		}
	}

	squash := false
	elidedCommits := 0
	if out == "" {
		// some repositories use squash merging(like insights-operator) and
		// out which is populated by --merges flag is empty. Thereby,
		// we are trying to get git log with --no-merges flag for that repositories
		// in order to get the logs.
		args = []string{"log", "--no-merges", "--topo-order", "-z", "--pretty=format:%H %P%x1E%ct%x1E%s%x1E%b", fmt.Sprintf("%s..%s", from, to)}
		out, err = g.exec(args...)
		if err != nil {
			return nil, 0, gitOutputToError(err, out)
		}
		squash = true
	} else {
		// some repositories use both real merges and squash or rebase merging.
		// Don't flood the output with single-commit noise, which might be poorly curated,
		// but do at least mention the fact that there are non-merge commits in the
		// first-parent line, so folks who are curious can click through to GitHub for
		// details.
		args := []string{"log", "--no-merges", "--first-parent", "-z", "--format=%H", fmt.Sprintf("%s..%s", from, to)}
		out, err := g.exec(args...)
		if err != nil {
			return nil, 0, gitOutputToError(err, out)
		}
		elidedCommits = strings.Count(out, "\x00")
	}

	if klog.V(5).Enabled() {
		klog.Infof("Got commit info:\n%s", strconv.Quote(out))
	}

	var commits []MergeCommit
	if len(out) == 0 {
		return nil, elidedCommits, nil
	}
	for _, entry := range strings.Split(out, "\x00") {
		records := strings.Split(entry, "\x1e")
		if len(records) != 4 {
			return nil, elidedCommits, fmt.Errorf("unexpected git log output width %d columns", len(records))
		}
		unixTS, err := strconv.ParseInt(records[1], 10, 64)
		if err != nil {
			return nil, elidedCommits, fmt.Errorf("unexpected timestamp: %v", err)
		}
		commitValues := strings.Split(records[0], " ")

		mergeCommit := MergeCommit{
			CommitDate:    time.Unix(unixTS, 0).UTC(),
			Commit:        commitValues[0],
			ParentCommits: commitValues[1:],
		}

		msg := records[3]
		if squash {
			msg = records[2]
		}

		mergeCommit.Refs, msg = extractRefs(msg)
		msg = strings.TrimSpace(msg)
		msg = strings.SplitN(msg, "\n", 2)[0]

		mergeMsg := records[2]
		var m []string
		if !squash {
			m = rePR.FindStringSubmatch(mergeMsg)
		} else {
			m = squashRePR.FindStringSubmatch(mergeMsg)
		}

		if m == nil || len(m) < 2 {
			klog.V(2).Infof("Omitted commit %s which has no pull-request", mergeCommit.Commit)
			continue
		}
		mergeCommit.PullRequest, err = strconv.Atoi(m[1])
		if err != nil {
			return nil, elidedCommits, fmt.Errorf("could not extract PR number from %q: %v", mergeMsg, err)
		}
		if len(msg) == 0 {
			msg = "Merge"
		}

		mergeCommit.Subject = msg
		commits = append(commits, mergeCommit)
	}

	return commits, elidedCommits, nil
}

// ensureCloneForRepo ensures that the repo exists on disk, is cloned, and has remotes for
// both repo and alternateRepos defined. The remotes for alternateRepos will be file system
// relative to avoid cloning repos twice.
func ensureCloneForRepo(dir string, repo string, alternateRepos []string, out, errOut io.Writer) (*git, error) {
	basePath, err := sourceLocationAsRelativePath(dir, repo)
	if err != nil {
		return nil, err
	}
	klog.V(4).Infof("Ensure repo is cloned at %s pointing to %s", basePath, repo)
	fi, err := os.Stat(basePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(basePath, 0777); err != nil {
			return nil, err
		}
	} else {
		if !fi.IsDir() {
			return nil, fmt.Errorf("repo path %s is not a directory", basePath)
		}
	}
	cloner := &git{}
	extractedRepo, err := cloner.ChangeContext(basePath)
	if err != nil {
		if err != noSuchRepo {
			return nil, err
		}
		klog.V(2).Infof("Cloning %s ...", repo)
		if err := extractedRepo.Clone(repo, out, errOut); err != nil {
			return nil, err
		}
	} else {
		if err := ensureFetchedRemoteForRepo(extractedRepo, repo); err != nil {
			return nil, err
		}
	}

	for _, altRepo := range alternateRepos {
		if altRepo == repo {
			continue
		}
		if err := ensureFetchedRemoteForRepo(extractedRepo, altRepo); err != nil {
			return nil, err
		}
	}

	return extractedRepo, nil
}

func remoteNameForRepo(repo string) string {
	sum := md5.Sum([]byte(repo))
	repoName := fmt.Sprintf("up-%s", base64.RawURLEncoding.EncodeToString(sum[:])[:10])
	return repoName
}

func ensureFetchedRemoteForRepo(g *git, repo string) error {
	repoName := remoteNameForRepo(repo)
	remoteOut, err := g.exec("remote", "add", repoName, repo)
	if !strings.Contains(remoteOut, "already exists") {
		if err != nil {
			return gitOutputToError(err, remoteOut)
		}
		if out, err := g.exec("fetch", "--filter=blob:none", repoName); err != nil {
			return gitOutputToError(err, out)
		}
	}
	return nil
}
