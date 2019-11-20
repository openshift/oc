package release

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"

	"golang.org/x/crypto/openpgp"

	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/MakeNowJust/heredoc"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
	"github.com/openshift/oc/pkg/cli/image/extract"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
)

// extractTarget describes how a file in the release image can be extracted to disk.
type extractTarget struct {
	OS       string
	Command  string
	Optional bool

	InjectReleaseImage   bool
	InjectReleaseVersion bool

	ArchiveFormat string
	AsArchive     bool
	AsZip         bool
	Readme        string
	LinkTo        []string

	Mapping extract.Mapping
}

var (
	readmeInstallUnix = heredoc.Doc(`
	# OpenShift Install

	The OpenShift installer \u0060openshift-install\u0060 makes it easy to get a cluster
	running on the public cloud or your local infrastructure.

	To learn more about installing OpenShift, visit [docs.openshift.com](https://docs.openshift.com)
	and select the version of OpenShift you are using.

	## Installing the tools

	After extracting this archive, you can move the \u0060openshift-install\u0060 binary
	to a location on your PATH such as \u0060/usr/local/bin\u0060, or keep it in a temporary
	directory and reference it via \u0060./openshift-install\u0060.

	## License

	OpenShift is licensed under the Apache Public License 2.0. The source code for this
	program is [located on github](https://github.com/openshift/installer).
	`)

	readmeCLIUnix = heredoc.Doc(`
	# OpenShift Clients

	The OpenShift client \u0060oc\u0060 simplifies working with Kubernetes and OpenShift
	clusters, offering a number of advantages over \u0060kubectl\u0060 such as easy login,
	kube config file management, and access to developer tools. The \u0060kubectl\u0060
	binary is included alongside for when strict Kubernetes compliance is necessary.

	To learn more about OpenShift, visit [docs.openshift.com](https://docs.openshift.com)
	and select the version of OpenShift you are using.

	## Installing the tools

	After extracting this archive, move the \u0060oc\u0060 and \u0060kubectl\u0060 binaries
	to a location on your PATH such as \u0060/usr/local/bin\u0060. Then run:

	    oc login [API_URL]

	to start a session against an OpenShift cluster. After login, run \u0060oc\u0060 and
	\u0060oc help\u0060 to learn more about how to get started with OpenShift.

	## License

	OpenShift is licensed under the Apache Public License 2.0. The source code for this
	program is [located on github](https://github.com/openshift/origin).
	`)

	readmeCLIWindows = heredoc.Doc(`
	# OpenShift Clients

	The OpenShift client \u0060oc.exe\u0060 simplifies working with Kubernetes and OpenShift
	clusters, offering a number of advantages over \u0060kubectl.exe\u0060 such as easy login,
	kube config file management, and access to developer tools.

	To learn more about OpenShift, visit [docs.openshift.com](https://docs.openshift.com)
	and select the version of OpenShift you are using.

	## Installing the tools

	After extracting this archive, move the \u0060oc.exe\u0060 binary	to a location on your
	PATH. Then run:

	    oc login [API_URL]

	to start a session against an OpenShift cluster. After login, run \u0060oc.exe\u0060 and
	\u0060oc.exe help\u0060 to learn more about how to get started with OpenShift.

	If you would like to use \u0060kubectl.exe\u0060 instead, copy the \u0060oc.exe\u0060 file
	and rename it to \u0060kubectl.exe\u0060. The interface will follow the conventions of that
	CLI.

	## License

	OpenShift is licensed under the Apache Public License 2.0. The source code for this
	program is [located on github](https://github.com/openshift/origin).
	`)
)

// extractCommand extracts specific commands out of images referenced by the release image.
// TODO: in the future the metadata this command contains might be loaded from the release
//   image, but we must maintain compatibility with older payloads if so
func (o *ExtractOptions) extractCommand(command string) error {
	// Available targets is treated as a GA API and may not be changed without backwards
	// compatibility of at least N-2 releases.
	availableTargets := []extractTarget{
		{
			OS:      "darwin",
			Command: "oc",
			Mapping: extract.Mapping{Image: "cli-artifacts", From: "usr/share/openshift/mac/oc"},

			LinkTo:               []string{"kubectl"},
			Readme:               readmeCLIUnix,
			InjectReleaseVersion: true,
			ArchiveFormat:        "openshift-client-mac-%s.tar.gz",
		},
		{
			OS:      "linux",
			Command: "oc",
			Mapping: extract.Mapping{Image: "cli", From: "usr/bin/oc"},

			LinkTo:               []string{"kubectl"},
			Readme:               readmeCLIUnix,
			InjectReleaseVersion: true,
			ArchiveFormat:        "openshift-client-linux-%s.tar.gz",
		},
		{
			OS:      "windows",
			Command: "oc",
			Mapping: extract.Mapping{Image: "cli-artifacts", From: "usr/share/openshift/windows/oc.exe"},

			Readme:               readmeCLIWindows,
			InjectReleaseVersion: true,
			ArchiveFormat:        "openshift-client-windows-%s.zip",
			AsZip:                true,
		},
		{
			OS:      "darwin",
			Command: "openshift-install",
			Mapping: extract.Mapping{Image: "installer-artifacts", From: "usr/share/openshift/mac/openshift-install"},

			Readme:             readmeInstallUnix,
			InjectReleaseImage: true,
			ArchiveFormat:      "openshift-install-mac-%s.tar.gz",
		},
		{
			OS:      "linux",
			Command: "openshift-install",
			Mapping: extract.Mapping{Image: "installer", From: "usr/bin/openshift-install"},

			Readme:             readmeInstallUnix,
			InjectReleaseImage: true,
			ArchiveFormat:      "openshift-install-linux-%s.tar.gz",
		},
		{
			OS:       "linux",
			Command:  "openshift-baremetal-install",
			Optional: true,
			Mapping:  extract.Mapping{Image: "baremetal-installer", From: "usr/bin/openshift-install"},

			Readme:             readmeInstallUnix,
			InjectReleaseImage: true,
			ArchiveFormat:      "openshift-baremetal-install-linux-%s.tar.gz",
		},
	}

	currentOS := runtime.GOOS
	if len(o.CommandOperatingSystem) > 0 {
		currentOS = o.CommandOperatingSystem
	}
	if currentOS == "mac" {
		currentOS = "darwin"
	}

	// Select the subset of targets based on command line input
	var willArchive bool
	var targets []extractTarget

	// Filter by command, or gather all non-optional targets
	if len(command) > 0 {
		for _, target := range availableTargets {
			if target.Command == command {
				targets = append(targets, target)
			}
		}
	} else {
		for _, target := range availableTargets {
			if !target.Optional {
				targets = append(targets, target)
			}
		}
	}

	// If the user didn't specify a command, or the operating system is set
	// to '*', we'll produce an archive
	if len(command) == 0 || o.CommandOperatingSystem == "*" {
		for i := range targets {
			targets[i].AsArchive = true
			targets[i].AsZip = targets[i].OS == "windows"
		}
	}

	if len(targets) == 0 {
		switch {
		case len(command) > 0 && currentOS != "*":
			return fmt.Errorf("command %q does not support the operating system %q", o.Command, currentOS)
		case len(command) > 0:
			return fmt.Errorf("the supported commands are 'oc' and 'openshift-install'")
		default:
			return fmt.Errorf("no available commands")
		}
	}

	var hashFn = sha256.New
	var signer *openpgp.Entity
	if willArchive && len(o.SigningKey) > 0 {
		key, err := ioutil.ReadFile(o.SigningKey)
		if err != nil {
			return err
		}
		keyring, err := openpgp.ReadArmoredKeyRing(bytes.NewBuffer(key))
		if err != nil {
			return err
		}
		for _, key := range keyring {
			if !key.PrivateKey.CanSign() {
				continue
			}
			fmt.Fprintf(o.Out, "Enter password for private key: ")
			password, err := terminal.ReadPassword(int(syscall.Stdin))
			fmt.Fprintln(o.Out)
			if err != nil {
				return err
			}
			if err := key.PrivateKey.Decrypt(password); err != nil {
				return fmt.Errorf("unable to decrypt signing key: %v", err)
			}
			for i, subkey := range key.Subkeys {
				if err := subkey.PrivateKey.Decrypt(password); err != nil {
					return fmt.Errorf("unable to decrypt signing subkey %d: %v", i, err)
				}
			}
			signer = key
			break
		}
		if signer == nil {
			return fmt.Errorf("no private key exists in %s capable of signing the output", o.SigningKey)
		}
	}

	// load the release image
	dir := o.Directory
	infoOptions := NewInfoOptions(o.IOStreams)
	infoOptions.SecurityOptions = o.SecurityOptions
	infoOptions.FileDir = o.FileDir
	release, err := infoOptions.LoadReleaseInfo(o.From, false)
	if err != nil {
		return err
	}
	releaseName := release.PreferredName()
	refExact := release.ImageRef
	refExact.Ref.Tag = ""
	refExact.Ref.ID = release.Digest.String()
	exactReleaseImage := refExact.String()

	// resolve target image references to their pull specs
	missing := sets.NewString()
	var validTargets []extractTarget
	for _, target := range targets {
		if currentOS != "*" && target.OS != currentOS {
			klog.V(2).Infof("Skipping %s, does not match current OS %s", target.ArchiveFormat, target.OS)
			continue
		}
		spec, err := findImageSpec(release.References, target.Mapping.Image, o.From)
		if err != nil {
			missing.Insert(target.Mapping.Image)
			continue
		}
		klog.V(2).Infof("Will extract %s from %s", target.Mapping.From, spec)
		ref, err := imagereference.Parse(spec)
		if err != nil {
			return err
		}
		target.Mapping.Image = spec
		target.Mapping.ImageRef = imagesource.TypedImageReference{Ref: ref, Type: imagesource.DestinationRegistry}
		if target.AsArchive {
			willArchive = true
			target.Mapping.Name = fmt.Sprintf(target.ArchiveFormat, releaseName)
			target.Mapping.To = filepath.Join(dir, target.Mapping.Name)
		} else {
			target.Mapping.To = filepath.Join(dir, target.Command)
			target.Mapping.Name = fmt.Sprintf("%s-%s", target.OS, target.Command)
		}
		validTargets = append(validTargets, target)
	}

	if len(validTargets) == 0 {
		if len(missing) == 1 {
			return fmt.Errorf("the image %q containing the desired command is not available", missing.List()[0])
		}
		return fmt.Errorf("some required images are missing: %s", strings.Join(missing.List(), ", "))
	}
	if len(missing) > 0 {
		fmt.Fprintf(o.ErrOut, "warning: Some commands can not be extracted due to missing images: %s\n", strings.Join(missing.List(), ", "))
	}

	// will extract in parallel
	opts := extract.NewOptions(genericclioptions.IOStreams{Out: o.Out, ErrOut: o.ErrOut})
	opts.ParallelOptions = o.ParallelOptions
	opts.SecurityOptions = o.SecurityOptions
	opts.OnlyFiles = true

	// create the mapping lookup of the valid targets
	var extractLock sync.Mutex
	targetsByName := make(map[string]extractTarget)
	for _, target := range validTargets {
		targetsByName[target.Mapping.Name] = target
		opts.Mappings = append(opts.Mappings, target.Mapping)
	}
	hashByTargetName := make(map[string]string)

	// ensure to is a directory
	if err := os.MkdirAll(dir, 0777); err != nil {
		return err
	}

	// as each layer is extracted, take the output binary and write it to disk
	opts.TarEntryCallback = func(hdr *tar.Header, layer extract.LayerInfo, r io.Reader) (bool, error) {
		// ensure we don't process the same mapping twice due to programmer error
		target, ok := func() (extractTarget, bool) {
			extractLock.Lock()
			defer extractLock.Unlock()
			target, ok := targetsByName[layer.Mapping.Name]
			return target, ok
		}()
		if !ok {
			return false, fmt.Errorf("unable to find target with mapping name %s", layer.Mapping.Name)
		}

		// open the file
		f, err := os.OpenFile(layer.Mapping.To, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return false, err
		}

		// if we need to write an archive, wrap the file appropriately to create a single
		// entry
		var w io.Writer = f

		bw := bufio.NewWriterSize(w, 16*1024)
		w = bw

		var hash hash.Hash
		closeFn := func() error { return nil }
		if target.AsArchive {
			text := strings.Replace(target.Readme, `\u0060`, "`", -1)
			hash = hashFn()
			w = io.MultiWriter(hash, w)
			if target.AsZip {
				klog.V(2).Infof("Writing %s as a ZIP archive %s", hdr.Name, layer.Mapping.To)
				zw := zip.NewWriter(w)

				if len(text) > 0 {
					text = strings.Replace(text, "\n", "\r\n", -1)
					zh := &zip.FileHeader{
						Method:             zip.Deflate,
						Name:               "README.md",
						UncompressedSize64: uint64(len(text)),
						Modified:           hdr.ModTime,
					}
					zh.SetMode(os.FileMode(0755))

					fw, err := zw.CreateHeader(zh)
					if err != nil {
						return false, err
					}
					if _, err := fmt.Fprintf(fw, text); err != nil {
						return false, err
					}
				}

				zh := &zip.FileHeader{
					Method:             zip.Deflate,
					Name:               target.Command + ".exe",
					UncompressedSize64: uint64(hdr.Size),
					Modified:           hdr.ModTime,
				}
				zh.SetMode(os.FileMode(0755))

				fw, err := zw.CreateHeader(zh)
				if err != nil {
					return false, err
				}

				w = fw
				closeFn = func() error { return zw.Close() }

			} else {
				klog.V(2).Infof("Writing %s as a tar.gz archive %s", hdr.Name, layer.Mapping.To)
				gw, err := gzip.NewWriterLevel(w, 3)
				if err != nil {
					return false, err
				}
				tw := tar.NewWriter(gw)

				if len(text) > 0 {
					if err := tw.WriteHeader(&tar.Header{
						Name:     "README.md",
						Mode:     int64(os.FileMode(0644).Perm()),
						Size:     int64(len(text)),
						Typeflag: tar.TypeReg,
						ModTime:  hdr.ModTime,
					}); err != nil {
						return false, err
					}
					if _, err := fmt.Fprintf(tw, text); err != nil {
						return false, err
					}
				}

				if err := tw.WriteHeader(&tar.Header{
					Name:     target.Command,
					Mode:     int64(os.FileMode(0755).Perm()),
					Size:     hdr.Size,
					Typeflag: tar.TypeReg,
					ModTime:  hdr.ModTime,
				}); err != nil {
					return false, err
				}

				w = tw
				closeFn = func() error {
					for _, link := range target.LinkTo {
						if err := tw.WriteHeader(&tar.Header{
							Name:     link,
							Mode:     int64(os.FileMode(0755).Perm()),
							Size:     0,
							Typeflag: tar.TypeLink,
							ModTime:  hdr.ModTime,
							Linkname: target.Command,
						}); err != nil {
							return err
						}
					}
					if err := tw.Close(); err != nil {
						return err
					}
					return gw.Close()
				}
			}
		}

		// copy the input to disk
		if target.InjectReleaseImage {
			var matched bool
			matched, err = copyAndReplaceReleaseInfo(w, r, 4*1024, exactReleaseImage)
			if !matched {
				fmt.Fprintf(o.ErrOut, "warning: Unable to replace release image location into %s, installer will not be locked to the correct image\n", target.Command)
			}
		} else if target.InjectReleaseVersion {
			var matched bool
			matched, err = copyAndReplaceReleaseVersion(w, r, 4*1024, releaseName)
			if !matched {
				fmt.Fprintf(o.ErrOut, "warning: Unable to replace release version location into %s\n", target.Command)
			}
		} else {
			_, err = io.Copy(w, r)
		}
		if err != nil {
			closeFn()
			f.Close()
			os.Remove(f.Name())
			return false, err
		}

		// ensure the file is written to disk
		if err := closeFn(); err != nil {
			return false, err
		}
		if err := bw.Flush(); err != nil {
			return false, err
		}
		if err := f.Close(); err != nil {
			return false, err
		}
		if err := os.Chtimes(f.Name(), hdr.ModTime, hdr.ModTime); err != nil {
			klog.V(2).Infof("Unable to set extracted file modification time: %v", err)
		}

		func() {
			extractLock.Lock()
			defer extractLock.Unlock()
			delete(targetsByName, layer.Mapping.Name)
			if hash != nil {
				hashByTargetName[layer.Mapping.To] = hex.EncodeToString(hash.Sum(nil))
			}
		}()

		return false, nil
	}
	if err := opts.Run(); err != nil {
		return err
	}

	if willArchive {
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, heredoc.Doc(`
			Client tools for OpenShift
			--------------------------
			
			These archives contain the client tooling for [OpenShift](https://docs.openshift.com).

			To verify the contents of this directory, use the 'gpg' and 'shasum' tools to
			ensure the archives you have downloaded match those published from this location.
			
			The openshift-install binary has been preconfigured to install the following release:

			---
			
		`))
		if err := describeReleaseInfo(buf, release, false, false, true, false); err != nil {
			return err
		}
		filename := "release.txt"
		if err := ioutil.WriteFile(filepath.Join(dir, filename), buf.Bytes(), 0644); err != nil {
			return err
		}
		hash := hashFn()
		hash.Write(buf.Bytes())
		hashByTargetName[filename] = hex.EncodeToString(hash.Sum(nil))
	}

	// write a checksum of the tar files to disk as sha256sum.txt.asc
	if len(hashByTargetName) > 0 {
		var keys []string
		for k := range hashByTargetName {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var lines []string
		for _, k := range keys {
			hash := hashByTargetName[k]
			lines = append(lines, fmt.Sprintf("%s  %s", hash, filepath.Base(k)))
		}
		// ensure a trailing newline
		if len(lines[len(lines)-1]) != 0 {
			lines = append(lines, "")
		}
		// write the content manifest
		data := []byte(strings.Join(lines, "\n"))
		filename := "sha256sum.txt"
		if err := ioutil.WriteFile(filepath.Join(dir, filename), data, 0644); err != nil {
			return fmt.Errorf("unable to write checksum file: %v", err)
		}
		// sign the content manifest
		if signer != nil {
			buf := &bytes.Buffer{}
			if err := openpgp.ArmoredDetachSign(buf, signer, bytes.NewBuffer(data), nil); err != nil {
				return fmt.Errorf("unable to sign the sha256sum.txt file: %v", err)
			}
			if err := ioutil.WriteFile(filepath.Join(dir, filename+".asc"), buf.Bytes(), 0644); err != nil {
				return fmt.Errorf("unable to write signed manifest: %v", err)
			}
		}
	}

	// if we did not process some targets, report that to the user and error if necessary
	if len(targetsByName) > 0 {
		var missing []string
		for _, target := range targetsByName {
			missing = append(missing, target.Mapping.From)
		}
		sort.Strings(missing)
		if len(missing) == 1 {
			return fmt.Errorf("image did not contain %s", missing[0])
		}
		return fmt.Errorf("unable to find multiple files: %s", strings.Join(missing, ", "))
	}

	return nil
}

const (
	// installerReplacement is the location within the installer binary that we can insert our release image info string
	installerReplacement = "\x00_RELEASE_IMAGE_LOCATION_\x00XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX\x00"
	// releaseVersionReplacement is the location within a binary (oc) that we can insert our release image name string
	releaseVersionReplacement = "\x00_RELEASE_VERSION_LOCATION_\x00XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX\x00\x00_RELEASE_VERSION_LOCATION_\x00XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX\x00"
)

// copyAndReplaceReleaseInfo performs a targeted replacement for binaries that contain a special marker string
// as a constant, replacing the marker with releaseInfo and a NUL terminating byte. It returns true if the
// replacement was performed.
func copyAndReplaceReleaseInfo(w io.Writer, r io.Reader, bufferSize int, releaseInfo string) (bool, error) {
	if len(releaseInfo)+1 > len(installerReplacement) {
		return false, fmt.Errorf("the release image pull spec is longer than the maximum replacement length for the binary")
	}
	if bufferSize < len(installerReplacement) {
		return false, fmt.Errorf("the buffer size must be greater than %d bytes", len(installerReplacement))
	}

	return copyReplace(w, r, bufferSize, releaseInfo, installerReplacement)
}

// copyAndReplaceReleaseVersion performs a targeted replacement for binaries that contain a special marker string
// as a constant, replacing the marker with releaseInfo and a NUL terminating byte. It returns true if the
// replacement was performed.
func copyAndReplaceReleaseVersion(w io.Writer, r io.Reader, bufferSize int, releaseInfo string) (bool, error) {
	if len(releaseInfo)+1 > len(releaseVersionReplacement) {
		return false, fmt.Errorf("the release image pull spec is longer than the maximum replacement length for the binary")
	}
	if bufferSize < len(releaseVersionReplacement) {
		return false, fmt.Errorf("the buffer size must be greater than %d bytes", len(releaseVersionReplacement))
	}

	return copyReplace(w, r, bufferSize, releaseInfo, releaseVersionReplacement)
}

func copyReplace(w io.Writer, r io.Reader, max int, releaseInfo, marker string) (bool, error) {
	match := []byte(marker[:len(releaseInfo)+1])
	offset := 0
	buf := make([]byte, max+offset)
	matched := false

	for {
		n, err := io.ReadFull(r, buf[offset:])

		// search in the buffer for the expected match
		end := offset + n
		if n > 0 {
			index := bytes.Index(buf[:end], match)
			if index != -1 {
				klog.V(2).Infof("Found match at %d (len=%d, offset=%d, n=%d)", index, len(buf), offset, n)
				// the replacement starts at the beginning of the match, contains the release string and a terminating NUL byte
				copy(buf[index:index+len(releaseInfo)], []byte(releaseInfo))
				buf[index+len(releaseInfo)] = 0x00
				matched = true
			}
		}

		// write everything that we have already searched (excluding the end of the buffer that will
		// be checked next pass)
		nextOffset := end - len(marker)
		if nextOffset < 0 || matched {
			nextOffset = 0
		}
		_, wErr := w.Write(buf[:end-nextOffset])
		if wErr != nil {
			return matched, wErr
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return matched, nil
			}
			return matched, err
		}

		// once we complete a single match, we can copy the rest of the file without processing
		if matched {
			_, err := io.Copy(w, r)
			return matched, err
		}

		// ensure the beginning of the buffer matches the end of the current buffer so that we
		// can search for matches that span buffers
		copy(buf[:nextOffset], buf[end-nextOffset:end])
		offset = nextOffset
	}
}
