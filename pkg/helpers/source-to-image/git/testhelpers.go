package git

import (
	"os"
	"path/filepath"

	"github.com/openshift/oc/pkg/helpers/source-to-image/cmd"
	"github.com/openshift/oc/pkg/helpers/source-to-image/cygpath"
)

// CreateLocalGitDirectory creates a git directory with a commit
func CreateLocalGitDirectory() (string, error) {
	cr := cmd.NewCommandRunner()

	dir, err := CreateEmptyLocalGitDirectory()
	if err != nil {
		return "", err
	}

	f, err := os.Create(filepath.Join(dir, "testfile"))
	if err != nil {
		return "", err
	}
	f.Close()

	err = cr.RunWithOptions(cmd.CommandOpts{Dir: dir}, "git", "add", ".")
	if err != nil {
		return "", err
	}

	err = cr.RunWithOptions(
		cmd.CommandOpts{
			Dir: dir,
			EnvAppend: []string{
				"GIT_AUTHOR_NAME=test",
				"GIT_AUTHOR_EMAIL=test@test",
				"GIT_COMMITTER_NAME=test",
				"GIT_COMMITTER_EMAIL=test@test",
			},
		},
		"git", "commit", "--no-gpg-sign", "-m", "testcommit")
	if err != nil {
		return "", err
	}

	return dir, nil
}

// CreateEmptyLocalGitDirectory creates a git directory with no checkin yet
func CreateEmptyLocalGitDirectory() (string, error) {
	cr := cmd.NewCommandRunner()

	dir, err := os.MkdirTemp(os.TempDir(), "gitdir-s2i-test")
	if err != nil {
		return "", err
	}

	err = cr.RunWithOptions(cmd.CommandOpts{Dir: dir}, "git", "init")
	if err != nil {
		return "", err
	}

	return dir, nil
}

// CreateLocalGitDirectoryWithSubmodule creates a git directory with a submodule
func CreateLocalGitDirectoryWithSubmodule() (string, error) {
	cr := cmd.NewCommandRunner()

	submodule, err := CreateLocalGitDirectory()
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(submodule)

	if cygpath.UsingCygwinGit {
		var err error
		submodule, err = cygpath.ToSlashCygwin(submodule)
		if err != nil {
			return "", err
		}
	}

	dir, err := CreateEmptyLocalGitDirectory()
	if err != nil {
		return "", err
	}

	// Allow submodule from file, see https://github.blog/2022-10-18-git-security-vulnerabilities-announced/#cve-2022-39253
	err = cr.RunWithOptions(cmd.CommandOpts{Dir: dir}, "git", "-c", "protocol.file.allow=always", "submodule", "add", submodule, "submodule")
	if err != nil {
		return "", err
	}

	return dir, nil
}
