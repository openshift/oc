package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openshift/api/annotations"
	dockerv10 "github.com/openshift/api/image/docker10"
	"github.com/openshift/library-go/pkg/image/imageutil"
	"github.com/openshift/oc/pkg/helpers/describe"
	imagehelpers "github.com/openshift/oc/pkg/helpers/image"
	"github.com/openshift/oc/pkg/helpers/newapp"
	"github.com/openshift/oc/pkg/helpers/newapp/app"
	"github.com/openshift/oc/pkg/helpers/newapp/portutils"
)

func displayName(meta metav1.ObjectMeta) string {
	// If an object has a display name, prefer it over the meta name.
	displayName := meta.Annotations[annotations.OpenShiftDisplayName]
	if len(displayName) > 0 {
		return displayName
	}
	return meta.Name
}

func localOrRemoteName(meta metav1.ObjectMeta, namespace string) string {
	if len(meta.Namespace) == 0 {
		return meta.Name
	}
	return meta.Namespace + "/" + meta.Name
}

func extractFirstImageStreamTag(newOnly bool, images ...*app.ImageRef) string {
	for _, image := range images {
		if image == nil {
			continue
		}
		if image.Exists() && newOnly {
			break
		}
		ref := image.ObjectReference()
		// if the reference is to an IST, the image is intended to be an IST
		if ref.Kind != "ImageStreamTag" || !image.AsImageStream {
			break
		}
		return ref.Name
	}
	return ""
}

func describeLocatedImage(refInput *app.ComponentInput, baseNamespace string) string {
	match := refInput.ResolvedMatch
	switch {
	case match == nil:
		return ""
	case match.ImageStream != nil:
		if image := match.DockerImage; image != nil {
			shortID := imagehelpers.ShortDockerImageID(image, 7)
			if !image.Created.IsZero() {
				shortID = fmt.Sprintf("%s (%s old)", shortID, describe.FormatRelativeTime(image.Created.Time))
			}
			return fmt.Sprintf("Found image %s in image stream %q under tag %q for %q", shortID, localOrRemoteName(match.ImageStream.ObjectMeta, baseNamespace), match.ImageTag, refInput)
		}
		return fmt.Sprintf("Found tag :%s in image stream %q for %q", match.ImageTag, localOrRemoteName(match.ImageStream.ObjectMeta, baseNamespace), refInput)
	case match.DockerImage != nil:
		image := match.DockerImage
		shortID := imagehelpers.ShortDockerImageID(image, 7)
		if !image.Created.IsZero() {
			shortID = fmt.Sprintf("%s (%s old)", shortID, describe.FormatRelativeTime(image.Created.Time))
		}
		return fmt.Sprintf("Found container image %s from %s for %q", shortID, match.Meta["registry"], refInput)
	default:
		return ""
	}
}

func inputAnnotations(match *app.ComponentMatch) map[string]string {
	if match == nil {
		return nil
	}
	base := make(map[string]string)
	if image := match.DockerImage; image != nil {
		if image.Config != nil {
			for k, v := range image.Config.Labels {
				base[k] = v
			}
		}
	}
	if stream := match.ImageStream; stream != nil && len(match.ImageTag) > 0 {
		if t, hasTag := imageutil.SpecHasTag(stream, match.ImageTag); hasTag {
			for k, v := range t.Annotations {
				base[k] = v
			}
		}
	}
	return base
}

func describeBuildPipelineWithImage(out io.Writer, ref app.ComponentReference, pipeline *app.Pipeline, baseNamespace string) {
	refInput := ref.Input()
	match := refInput.ResolvedMatch

	if locatedImage := describeLocatedImage(refInput, baseNamespace); len(locatedImage) > 0 {
		fmt.Fprintf(out, "--> %s\n", locatedImage)
		annotations := inputAnnotations(refInput.ResolvedMatch)
		if desc := annotations["io.k8s.display-name"]; len(desc) > 0 {
			fmt.Fprintln(out)
			fmt.Fprintf(out, "    %s \n", desc)
			fmt.Fprintf(out, "    %s \n", strings.Repeat("-", len(desc)))
		} else {
			fmt.Fprintln(out)
		}
		if desc := annotations["io.k8s.description"]; len(desc) > 0 {
			fmt.Fprintf(out, "    %s\n\n", desc)
		}
		if desc := annotations["io.openshift.tags"]; len(desc) > 0 {
			desc = strings.Join(strings.Split(desc, ","), ", ")
			fmt.Fprintf(out, "    Tags: %s\n\n", desc)
		}
	}

	if pipeline.Build == nil {
		trackedImage := extractFirstImageStreamTag(true, pipeline.InputImage, pipeline.Image)
		if len(trackedImage) > 0 {
			fmt.Fprintf(out, "    * An image stream tag will be created as %q that will track this image\n", trackedImage)
		}
	} else {
		trackedImage := extractFirstImageStreamTag(true, pipeline.InputImage)
		if len(trackedImage) > 0 {
			fmt.Fprintf(out, "    * An image stream tag will be created as %q that will track the source image\n", trackedImage)
		}
		if refInput.Uses != nil && refInput.Uses.Info() != nil {
			matches := []string{}
			for _, t := range refInput.Uses.Info().Types {
				if len(t.Platform) == 0 {
					continue
				}
				if len(t.Version) > 0 {
					matches = append(matches, fmt.Sprintf("%s %s", t.Platform, t.Version))
				}
				matches = append(matches, t.Platform)
			}
			if len(matches) > 0 && pipeline.Build.Strategy.Strategy == newapp.StrategySource {
				fmt.Fprintf(out, "    * The source repository appears to match: %s\n", strings.Join(matches, ", "))
			}
		}
		noSource := false
		var source string
		switch s := pipeline.Build.Source; {
		case s.Binary:
			noSource = true
			source = "binary input"
		case len(s.DockerfileContents) > 0:
			source = "a predefined Dockerfile"
		case s.URL != nil && len(s.URL.URL.Host) > 0:
			source = fmt.Sprintf("source code from %s", s.URL)
		case s.URL != nil:
			noSource = true
			source = "uploaded code"
		default:
			source = "<unknown>"
		}

		fmt.Fprintf(out, "    * A %s build using %s will be created\n", pipeline.Build.Strategy.Strategy, source)
		if buildOut, err := pipeline.Build.Output.BuildOutput(); err == nil && buildOut != nil && buildOut.To != nil {
			switch to := buildOut.To; {
			case to.Kind == "ImageStreamTag":
				fmt.Fprintf(out, "      * The resulting image will be pushed to image stream tag %q\n", to.Name)
			case to.Kind == "DockerImage":
				fmt.Fprintf(out, "      * The resulting image will be pushed with Docker to %q\n", to.Name)
			default:
				fmt.Fprintf(out, "      * The resulting image will be pushed to %s %q\n", to.Kind, to.Name)
			}
		}

		if noSource {
			// if we have no source, the user must always provide the source from the local dir(binary build)
			fmt.Fprintf(out, "      * A binary build was created, use 'arvan paas start-build --from-dir' to trigger a new build\n")
		} else {
			if len(trackedImage) > 0 {
				// if we have a trackedImage/ICT and we have source, the build will be triggered automatically.
				fmt.Fprintf(out, "      * Every time %q changes a new build will be triggered\n", trackedImage)
			} else {
				// if we have source (but not a tracked image), the user must manually trigger a build.
				fmt.Fprintf(out, "      * Use 'arvan paas start-build' to trigger a new build\n")
			}
		}

		if pipeline.Build.Source.RequiresAuth {
			fmt.Fprintf(out, "      * WARNING: this source repository may require credentials.\n"+
				"                 Create a secret with your git credentials and use 'arvan paas set build-secret' to assign it to the build config.\n")
		}
	}
	if pipeline.DeploymentConfig != nil {
		if pipeline.DeploymentConfig.AsTest {
			if len(pipeline.DeploymentConfig.Images) > 1 {
				fmt.Fprintf(out, "    * This image will be test deployed as part of deployment config %q\n", pipeline.DeploymentConfig.Name)
			} else {
				fmt.Fprintf(out, "    * This image will be test deployed in deployment config %q\n", pipeline.DeploymentConfig.Name)
			}
		} else {
			if len(pipeline.DeploymentConfig.Images) > 1 {
				fmt.Fprintf(out, "    * This image will be deployed as part of deployment config %q\n", pipeline.DeploymentConfig.Name)
			} else {
				fmt.Fprintf(out, "    * This image will be deployed in deployment config %q\n", pipeline.DeploymentConfig.Name)
			}
		}
	}
	if match != nil && match.DockerImage != nil {
		if pipeline.DeploymentConfig != nil {
			ports := sets.NewString()
			if match.DockerImage.Config != nil {
				for k := range match.DockerImage.Config.ExposedPorts {
					ports.Insert(k)
				}
			}
			orderedPorts := ports.List()
			sort.Sort(sort.StringSlice(orderedPorts))
			filteredPorts, errs := portutils.FilterPortAndProtocolArray(orderedPorts)
			if len(errs) > 0 {
				fmt.Fprintf(out, "    * WARNING: The Dockerfile has some invalid ports in EXPOSE instruction: %v\n", errs)
			}
			switch len(filteredPorts) {
			case 0:
				fmt.Fprintf(out, "    * The image does not expose any ports - if you want to load balance or send traffic to this component\n")
				fmt.Fprintf(out, "      you will need to create a service with 'arvan paas expose dc/%s --port=[port]' later\n", pipeline.DeploymentConfig.Name)
			default:
				filteredPortsString := make([]string, len(filteredPorts))
				for i, dp := range filteredPorts {
					filteredPortsString[i] = dp.Port() + `/` + dp.Proto()
				}
				if len(filteredPorts) == 1 {
					fmt.Fprintf(out, "    * Port %s will be load balanced by service %q\n", filteredPortsString[0], pipeline.DeploymentConfig.Name)
				} else {
					fmt.Fprintf(out, "    * Ports %s will be load balanced by service %q\n", strings.Join(filteredPortsString, ", "), pipeline.DeploymentConfig.Name)
				}
				fmt.Fprintf(out, "      * Other containers can access this service through the hostname %q\n", pipeline.DeploymentConfig.Name)
			}
			if hasEmptyDir(match.DockerImage) {
				fmt.Fprintf(out, "    * This image declares volumes and will default to use non-persistent, host-local storage.\n")
				fmt.Fprintf(out, "      You can add persistent volumes later by running 'arvan paas set volume dc/%s --add ...'\n", pipeline.DeploymentConfig.Name)
			}
			if hasRootUser(match.DockerImage) {
				fmt.Fprintf(out, "    * WARNING: Image %q runs as the 'root' user which may not be permitted by your cluster administrator\n", match.Name)
			}
		}
	}
	fmt.Fprintln(out)
}

func hasRootUser(image *dockerv10.DockerImage) bool {
	if image.Config == nil {
		return false
	}
	return len(image.Config.User) == 0 || image.Config.User == "root" || image.Config.User == "0"
}

func hasEmptyDir(image *dockerv10.DockerImage) bool {
	if image.Config == nil {
		return false
	}
	return len(image.Config.Volumes) > 0
}

func describeGeneratedJob(out io.Writer, ref app.ComponentReference, pod *corev1.Pod, secret *corev1.Secret, baseNamespace string) {
	refInput := ref.Input()
	generatorInput := refInput.ResolvedMatch.GeneratorInput
	hasToken := generatorInput.Token != nil

	fmt.Fprintf(out, "--> Installing application from %q\n", refInput)
	if locatedImage := describeLocatedImage(refInput, baseNamespace); len(locatedImage) > 0 {
		fmt.Fprintf(out, "    * %s\n", locatedImage)
	}

	fmt.Fprintf(out, "    * Install will run in pod %q\n", localOrRemoteName(pod.ObjectMeta, baseNamespace))
	switch {
	case secret != nil:
		fmt.Fprintf(out, "    * The pod has access to your current session token through the secret %q.\n", localOrRemoteName(secret.ObjectMeta, baseNamespace))
		fmt.Fprintf(out, "      If you cancel the install, you should delete the secret or log out of your session.\n")
	case hasToken && generatorInput.Token.Env != nil:
		fmt.Fprintf(out, "    * The pod has access to your current session token via environment variable %s.\n", *generatorInput.Token.Env)
		fmt.Fprintf(out, "      If you cancel the install, you should delete the pod or log out of your session.\n")
	case hasToken && generatorInput.Token.ServiceAccount:
		fmt.Fprintf(out, "    * The pod will use the 'installer' service account. If this account does not exist\n")
		fmt.Fprintf(out, "      with sufficient permissions, you may need to ask a project admin set it up.\n")
	case hasToken:
		fmt.Fprintf(out, "    * The pod has access to your current session token. Please delete the pod if you cancel the install.\n")
	}
	if hasToken {
		if generatorInput.Token.ServiceAccount {
			fmt.Fprintf(out, "--> WARNING: The pod requires access to the 'installer' service account to install this\n")
			fmt.Fprintf(out, "      image. Only grant access to images whose source you trust. The image will be able\n")
			fmt.Fprintf(out, "      to act as an editor within this project.\n")
		} else {
			fmt.Fprintf(out, "--> WARNING: The pod requires access to your current session token to install this image. Only\n")
			fmt.Fprintf(out, "      grant access to images whose source you trust. The image will be able to perform any\n")
			fmt.Fprintf(out, "      action you can take on the cluster.\n")
		}
	}
}
