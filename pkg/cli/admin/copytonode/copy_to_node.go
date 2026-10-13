package copytonode

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"al.essio.dev/pkg/shellescape"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/oc/pkg/cli/admin/pernodepod"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	//go:embed copy-pod-template.yaml
	podYaml []byte
	pod     = resourceread.ReadPodV1OrDie(podYaml)
)

type CopyToNodeRuntime struct {
	PerNodePodRuntime *pernodepod.PerNodePodRuntime

	FileSources []string
}

func (r *CopyToNodeRuntime) Run(ctx context.Context) error {
	secret, secretDataKeyToFilename, err := r.secretFromFileSources()
	if err != nil {
		return fmt.Errorf("unable to create secret: %w", err)
	}
	secret.Name = "source-data"
	secret.Namespace = r.PerNodePodRuntime.NamespacePrefix
	r.PerNodePodRuntime.KubeClient.CoreV1().Secrets(secret.Namespace).Create(ctx, secret, metav1.CreateOptions{})

	prePodHookFn := func(ctx context.Context, namespaceName string) (pernodepod.CleanUpFunc, error) {
		secret.Name = "source-data"
		secret.Namespace = namespaceName
		if _, err := r.PerNodePodRuntime.KubeClient.CoreV1().Secrets(secret.Namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			return nil, fmt.Errorf("failed to create source-data: %w", err)
		}
		fmt.Fprintf(r.PerNodePodRuntime.Out, "Created --namespace=%v secrets/%v\n", namespaceName, secret.Name)

		cleanupFn := func(ctx context.Context) {
			if err := r.PerNodePodRuntime.KubeClient.CoreV1().Secrets(secret.Namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil {
				fmt.Fprintf(r.PerNodePodRuntime.ErrOut, "failed to cleanup source-data: %v", err)
			}
		}
		return cleanupFn, nil
	}

	copyCommands := []string{
		"#/bin/bash",
		"set -uo pipefail",
	}
	for source, destination := range secretDataKeyToFilename {
		sourcePath := filepath.Join("/source-data", source)
		destPath := filepath.Join("/host-root", destination)
		parentDir := filepath.Dir(destPath)
		copyCommands = append(copyCommands, fmt.Sprintf("mkdir -p %s", shellescape.Quote(parentDir)))
		copyCommands = append(copyCommands, fmt.Sprintf("cp --dereference -fr %s %s", shellescape.Quote(sourcePath), shellescape.Quote(destPath)))
	}

	createPodFn := func(ctx context.Context, namespaceName, nodeName, imagePullSpec string) (*corev1.Pod, error) {
		restartObj := pod.DeepCopy()
		restartObj.Namespace = namespaceName
		restartObj.Spec.NodeName = nodeName
		restartObj.Spec.Containers[0].Image = imagePullSpec
		restartObj.Spec.Containers[0].Command = append(
			restartObj.Spec.Containers[0].Command,
			strings.Join(copyCommands, "\n"))
		return restartObj, nil
	}

	return r.PerNodePodRuntime.Run(ctx, prePodHookFn, createPodFn)
}

// lifted from create secret command
// returns a map of secret key data to the node file location
func (r *CopyToNodeRuntime) secretFromFileSources() (*corev1.Secret, map[string]string, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
		Data: map[string][]byte{},
	}
	secretDataKeyToFilename := map[string]string{}

	for i, fileSource := range r.FileSources {
		sourcePath, nodeDestination, err := parseFileSource(fileSource)
		if err != nil {
			return nil, nil, err
		}

		fileInfo, err := os.Stat(sourcePath)
		if err != nil {
			switch err := err.(type) {
			case *os.PathError:
				return nil, nil, fmt.Errorf("error reading %s: %w", sourcePath, err.Err)
			default:
				return nil, nil, fmt.Errorf("error reading %s: %w", sourcePath, err)
			}
		}

		// if the filePath is a directory
		if fileInfo.IsDir() {
			fileList, err := ioutil.ReadDir(sourcePath)
			if err != nil {
				return nil, nil, fmt.Errorf("error listing files in %s: %v", sourcePath, err)
			}
			for j, item := range fileList {
				itemPath := path.Join(sourcePath, item.Name())
				if item.Mode().IsRegular() {
					secretDataKey := fmt.Sprintf("copy-to-node-%d-%d", i, j)
					fileContent, err := os.ReadFile(itemPath)
					if err != nil {
						return nil, nil, fmt.Errorf("error reading %s: %w", sourcePath, err)
					}

					itemNodeDestination := filepath.Join(nodeDestination, item.Name())
					secretDataKeyToFilename[secretDataKey] = itemNodeDestination
					secret.Annotations[secretDataKey] = itemNodeDestination
					secret.Data[secretDataKey] = fileContent
				}
			}

		} else {
			// if the filepath is a file
			secretDataKey := fmt.Sprintf("copy-to-node-%d", i)
			fileContent, err := os.ReadFile(sourcePath)
			if err != nil {
				return nil, nil, fmt.Errorf("error reading %s: %w", sourcePath, err)
			}

			secretDataKeyToFilename[secretDataKey] = nodeDestination
			secret.Annotations[secretDataKey] = nodeDestination
			secret.Data[secretDataKey] = fileContent
		}

	}

	return secret, secretDataKeyToFilename, nil
}

// parseFileSource parses the source given.
// source-path=node-destination
func parseFileSource(source string) (sourcePath, nodeDestination string, err error) {
	numSeparators := strings.Count(source, "=")
	switch {
	case numSeparators == 1 && strings.HasPrefix(source, "="):
		return "", "", fmt.Errorf("source-path is %v missing", strings.TrimPrefix(source, "="))
	case numSeparators == 1 && strings.HasSuffix(source, "="):
		return "", "", fmt.Errorf("node-destination is %v missing", strings.TrimSuffix(source, "="))
	case numSeparators != 1:
		return "", "", errors.New("format is <source-path>=<node-destination>")
	default:
		components := strings.Split(source, "=")
		return components[0], components[1], nil
	}
}
