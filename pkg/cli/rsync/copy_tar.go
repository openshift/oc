package rsync

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	s2ifs "github.com/openshift/oc/pkg/helpers/source-to-image/fs"
	"github.com/openshift/oc/pkg/helpers/source-to-image/tar"
)

// tarStrategy implements the tar copy strategy.
// The tar strategy consists of creating a tar of the file contents to copy
// and then streaming them to/from the container to the destination to a tar
// command waiting for STDIN input. If the --delete flag is specified, the
// contents of the destination directory are first cleared before the copy.
// The tar strategy requires that the remote container contain the tar command.
type tarStrategy struct {
	Quiet          bool
	Delete         bool
	Tar            tar.Tar
	RemoteExecutor executor
	Includes       []string
	Excludes       []string
	IgnoredFlags   []string
	Flags          []string
}

// NewTarStrategy returns a copy strategy that uses tar.
func NewTarStrategy(o *RsyncOptions) CopyStrategy {

	tarHelper := tar.New(s2ifs.NewFileSystem())
	tarHelper.SetExclusionPattern(nil)

	ignoredFlags := rsyncSpecificFlags(o)

	remoteExec := newRemoteExecutor(o)

	return &tarStrategy{
		Quiet:          o.Quiet,
		Delete:         o.Delete,
		Includes:       o.RsyncInclude,
		Excludes:       o.RsyncExclude,
		Tar:            tarHelper,
		RemoteExecutor: remoteExec,
		IgnoredFlags:   ignoredFlags,
		Flags:          tarFlagsFromOptions(o),
	}
}

func deleteContents(dir string) error {
	klog.V(4).Infof("Deleting local directory contents: %s", dir)
	files, err := os.ReadDir(dir)
	if err != nil {
		klog.V(4).Infof("Could not read directory %s: %v", dir, err)
		return err
	}
	for _, f := range files {
		if f.IsDir() {
			klog.V(5).Infof("Deleting directory: %s", f.Name())
			err = os.RemoveAll(filepath.Clean(filepath.Join(dir, f.Name())))
		} else {
			klog.V(5).Infof("Deleting file: %s", f.Name())
			err = os.Remove(filepath.Clean(filepath.Join(dir, f.Name())))
		}
		if err != nil {
			klog.V(4).Infof("Error deleting file or directory: %s: %v", f.Name(), err)
			return err
		}
	}
	return nil
}

func deleteLocal(source, dest *PathSpec) error {
	deleteDir := dest.Path
	// Determine which directory to empty based on source parameter
	// If the source does not end in a path separator, the directory
	// being copied over is the directory that needs to be cleaned out
	// in the destination. This is to replicate the behavior of the
	// rsync --delete flag
	if !strings.HasSuffix(source.Path, "/") {
		deleteDir = filepath.Join(deleteDir, filepath.Base(source.Path))
	}
	return deleteContents(deleteDir)
}

func deleteRemote(source, dest *PathSpec, ex executor) error {
	// Determine which directory to empty based on source parameter
	// If the source does not end in a path separator, the directory
	// being copied over is the directory that needs to be cleaned out
	// in the destination. This is to replicate the behavior of the
	// rsync --delete flag
	deleteDir := dest.Path
	if !strings.HasSuffix(source.Path, string(filepath.Separator)) {
		deleteDir = path.Join(deleteDir, path.Base(source.Path))
	}
	deleteCmd := []string{"sh", "-c", fmt.Sprintf("shopt -s dotglob && rm -rf %s", path.Join(deleteDir, "*"))}
	return executeWithLogging(ex, deleteCmd)
}

func deleteFiles(source, dest *PathSpec, remoteExecutor executor) error {
	if dest.Local() {
		return deleteLocal(source, dest)
	}
	return deleteRemote(source, dest, remoteExecutor)
}

func (r *tarStrategy) Copy(source, destination *PathSpec, out, errOut io.Writer) error {

	klog.V(3).Infof("Copying files with tar")

	if len(r.IgnoredFlags) > 0 {
		fmt.Fprintf(errOut, "Ignoring the following flags because they only apply to rsync: %s\n", strings.Join(r.IgnoredFlags, ", "))
	}

	if r.Delete {
		// Implement the rsync --delete flag as a separate call to first delete directory contents
		err := deleteFiles(source, destination, r.RemoteExecutor)
		if err != nil {
			return fmt.Errorf("unable to delete files in destination: %v", err)
		}
	}
	tmp, err := os.CreateTemp("", "rsync")
	if err != nil {
		return fmt.Errorf("cannot create local temporary file for tar: %v", err)
	}
	defer tmp.Close()
	defer os.Remove(tmp.Name())

	// Create tar
	if source.Local() {
		klog.V(4).Infof("Creating local tar file %s from local path %s", tmp.Name(), source.Path)
		err = tarLocal(r.Tar, source.Path, tmp)
		if err != nil {
			return fmt.Errorf("error creating local tar of source directory: %v", err)
		}
	} else {
		klog.V(4).Infof("Creating local tar file %s from remote path %s", tmp.Name(), source.Path)
		errBuf := &bytes.Buffer{}
		err = tarRemote(r.RemoteExecutor, source.Path, r.Includes, r.Excludes, tmp, errBuf)
		if err != nil {
			if checkTar(r.RemoteExecutor) != nil {
				return strategySetupError("tar not available in container")
			}
			io.Copy(errOut, errBuf)
			return fmt.Errorf("error creating remote tar of source directory: %v", err)
		}
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("error resetting position in a temporary tar file %s: %v", tmp.Name(), err)
	}

	// Extract tar
	if destination.Local() {
		klog.V(4).Infof("Untarring temp file %s to local directory %s", tmp.Name(), destination.Path)
		err = untarLocal(r.Tar, destination.Path, tmp, r.Quiet, out)
	} else {
		klog.V(4).Infof("Untarring temp file %s to remote directory %s", tmp.Name(), destination.Path)
		errBuf := &bytes.Buffer{}
		err = untarRemote(r.RemoteExecutor, destination.Path, r.Flags, tmp, out, errBuf)
		if err != nil {
			if checkTar(r.RemoteExecutor) != nil {
				return strategySetupError("tar not available in container")
			}
			io.Copy(errOut, errBuf)
		}
	}
	if err != nil {
		return fmt.Errorf("error extracting tar at destination directory: %v", err)
	}
	return nil
}

func (r *tarStrategy) Validate() error {
	errs := []error{}
	if r.Tar == nil {
		errs = append(errs, errors.New("tar helper must be provided"))
	}
	if r.RemoteExecutor == nil {
		errs = append(errs, errors.New("remote executor must be provided"))
	}
	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}
	return nil
}

func (r *tarStrategy) String() string {
	return "tar"
}

func tarRemote(exec executor, sourceDir string, includes, excludes []string, out, errOut io.Writer) error {
	klog.V(4).Infof("Tarring %s remotely", sourceDir)

	exclude := []string{}
	for _, pattern := range excludes {
		exclude = append(exclude, fmt.Sprintf("--exclude=%s", pattern))
	}

	var cmd []string
	if strings.HasSuffix(sourceDir, "/") {
		include := []string{"."}
		include = append(include, includes...)

		cmd = []string{"tar", "-C", sourceDir, "-c"}
		cmd = append(cmd, append(include, exclude...)...)
	} else {
		include := []string{}

		for _, pattern := range includes {
			include = append(include, path.Join(path.Base(sourceDir), pattern))
		}

		cmd = []string{"tar", "-C", path.Dir(sourceDir), "-c", path.Base(sourceDir)}
		cmd = append(cmd, append(include, exclude...)...)
	}
	klog.V(4).Infof("Remote tar command: %s", strings.Join(cmd, " "))
	return exec.Execute(cmd, nil, out, errOut)
}

func tarLocal(tar tar.Tar, sourceDir string, w io.Writer) error {
	klog.V(4).Infof("Tarring %s locally", sourceDir)
	// includeParent mimics rsync's behavior. When the source path ends in a path
	// separator, then only the contents of the directory are copied. Otherwise,
	// the directory itself is copied.
	includeParent := true
	if strings.HasSuffix(sourceDir, string(filepath.Separator)) {
		includeParent = false
		sourceDir = sourceDir[:len(sourceDir)-1]
	}
	return tar.CreateTarStream(sourceDir, includeParent, w)
}

func untarLocal(tar tar.Tar, destinationDir string, r io.Reader, quiet bool, logger io.Writer) error {
	klog.V(4).Infof("Extracting tar locally to %s", destinationDir)
	if quiet {
		return tar.ExtractTarStream(destinationDir, r)
	}
	return tar.ExtractTarStreamWithLogging(destinationDir, r, logger)
}

func untarRemote(exec executor, destinationDir string, flags []string, in io.Reader, out, errOut io.Writer) error {
	cmd := []string{"tar", "-C", destinationDir, "-ox"}
	cmd = append(cmd, flags...)
	klog.V(4).Infof("Extracting tar remotely with command: %s", strings.Join(cmd, " "))
	return exec.Execute(cmd, in, out, errOut)
}
