package set

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	probeLong = templates.LongDesc(`
		Set or remove a liveness, readiness or startup probe from a pod or pod template

		Each container in a pod may define one or more probes that are used for general health
		checking. A liveness probe is checked periodically to ensure the container is still healthy:
		if the probe fails, the container is restarted. Readiness probes set or clear the ready
		flag for each container, which controls whether the container's ports are included in the list
		of endpoints for a service and whether a deployment can proceed. A readiness check should
		indicate when your container is ready to accept incoming traffic or begin handling work.
		A startup probe allows additional startup time for a container before a liveness probe
		is started.
		Setting both liveness and readiness probes for each container is highly recommended.

		The three probe types are:

		1. Open a TCP socket on the pod IP
		2. Perform an HTTP GET against a URL on a container that must return 200 OK
		3. Run a command in the container that must return exit code 0

		Containers that take a variable amount of time to start should set generous
		initial-delay-seconds values, otherwise as your application evolves you may suddenly begin
		to fail.`)

	probeExample = templates.Examples(`
		# Clear both readiness and liveness probes off all containers
		arvan paas set probe dc/myapp --remove --readiness --liveness

		# Set an exec action as a liveness probe to run 'echo ok'
		arvan paas set probe dc/myapp --liveness -- echo ok

		# Set a readiness probe to try to open a TCP socket on 3306
		arvan paas set probe rc/mysql --readiness --open-tcp=3306

		# Set an HTTP startup probe for port 8080 and path /healthz over HTTP on the pod IP
		arvan paas probe dc/webapp --startup --get-url=http://:8080/healthz

		# Set an HTTP readiness probe for port 8080 and path /healthz over HTTP on the pod IP
		arvan paas probe dc/webapp --readiness --get-url=http://:8080/healthz

		# Set an HTTP readiness probe over HTTPS on 127.0.0.1 for a hostNetwork pod
		arvan paas set probe dc/router --readiness --get-url=https://127.0.0.1:1936/stats

		# Set only the initial-delay-seconds field on all deployments
		arvan paas set probe dc --all --readiness --initial-delay-seconds=30
	`)
)

type ProbeOptions struct {
	PrintFlags *genericclioptions.PrintFlags

	ContainerSelector string
	Selector          string
	All               bool
	Readiness         bool
	Liveness          bool
	Startup           bool
	Remove            bool
	Local             bool
	OpenTCPSocket     string
	HTTPGet           string

	Mapper                 meta.RESTMapper
	Client                 dynamic.Interface
	Printer                printers.ResourcePrinter
	Builder                func() *resource.Builder
	Encoder                runtime.Encoder
	Namespace              string
	ExplicitNamespace      bool
	UpdatePodSpecForObject polymorphichelpers.UpdatePodSpecForObjectFunc
	Command                []string
	Resources              []string
	DryRunStrategy         kcmdutil.DryRunStrategy

	FlagSet       func(string) bool
	HTTPGetAction *corev1.HTTPGetAction

	// Length of time before health checking is activated.  In seconds.
	InitialDelaySeconds *int
	// Length of time before health checking times out.  In seconds.
	TimeoutSeconds *int
	// How often (in seconds) to perform the probe.
	PeriodSeconds *int
	// Minimum consecutive successes for the probe to be considered successful after having failed.
	// Must be 1 for liveness.
	SuccessThreshold *int
	// Minimum consecutive failures for the probe to be considered failed after having succeeded.
	FailureThreshold *int

	resource.FilenameOptions
	genericclioptions.IOStreams
}

func NewProbeOptions(streams genericclioptions.IOStreams) *ProbeOptions {
	return &ProbeOptions{
		PrintFlags: genericclioptions.NewPrintFlags("probes updated").WithTypeSetter(scheme.Scheme),
		IOStreams:  streams,

		ContainerSelector: "*",
	}
}

// NewCmdProbe implements the set probe command
func NewCmdProbe(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewProbeOptions(streams)
	cmd := &cobra.Command{
		Use:     "probe RESOURCE/NAME --readiness|--liveness [flags] (--get-url=URL|--open-tcp=PORT|-- CMD)",
		Short:   "Update a probe on a pod template",
		Long:    probeLong,
		Example: probeExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	usage := "to use to edit the resource"
	kcmdutil.AddFilenameOptionFlags(cmd, &o.FilenameOptions, usage)
	cmd.Flags().StringVarP(&o.ContainerSelector, "containers", "c", o.ContainerSelector, "The names of containers in the selected pod templates to change - may use wildcards")
	cmd.Flags().StringVarP(&o.Selector, "selector", "l", o.Selector, "Selector (label query) to filter on")
	cmd.Flags().BoolVar(&o.All, "all", o.All, "If true, select all resources in the namespace of the specified resource types")
	cmd.Flags().BoolVar(&o.Remove, "remove", o.Remove, "If true, remove the specified probe(s).")
	cmd.Flags().BoolVar(&o.Readiness, "readiness", o.Readiness, "Set or remove a readiness probe to indicate when this container should receive traffic")
	cmd.Flags().BoolVar(&o.Liveness, "liveness", o.Liveness, "Set or remove a liveness probe to verify this container is running")
	cmd.Flags().BoolVar(&o.Startup, "startup", o.Startup, "Set or remove a startup probe to verify this container is running")
	cmd.Flags().BoolVar(&o.Local, "local", o.Local, "If true, set image will NOT contact api-server but run locally.")
	cmd.Flags().StringVar(&o.OpenTCPSocket, "open-tcp", o.OpenTCPSocket, "A port number or port name to attempt to open via TCP.")
	cmd.Flags().StringVar(&o.HTTPGet, "get-url", o.HTTPGet, "A URL to perform an HTTP GET on (you can omit the host, have a string port, or omit the scheme.")

	o.InitialDelaySeconds = cmd.Flags().Int("initial-delay-seconds", 0, "The time in seconds to wait before the probe begins checking")
	o.SuccessThreshold = cmd.Flags().Int("success-threshold", 0, "The number of successes required before the probe is considered successful")
	o.FailureThreshold = cmd.Flags().Int("failure-threshold", 0, "The number of failures before the probe is considered to have failed")
	o.PeriodSeconds = cmd.Flags().Int("period-seconds", 0, "The time in seconds between attempts")
	o.TimeoutSeconds = cmd.Flags().Int("timeout-seconds", 0, "The time in seconds to wait before considering the probe to have failed")

	o.PrintFlags.AddFlags(cmd)
	kcmdutil.AddDryRunFlag(cmd)

	return cmd
}

func (o *ProbeOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	o.Resources = args
	if i := cmd.ArgsLenAtDash(); i != -1 {
		o.Resources = args[:i]
		o.Command = args[i:]
	}
	if len(o.Filenames) == 0 && len(args) < 1 {
		return kcmdutil.UsageErrorf(cmd, "one or more resources must be specified as <resource> <name> or <resource>/<name>")
	}

	var err error
	o.Namespace, o.ExplicitNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.Mapper, err = f.ToRESTMapper()
	if err != nil {
		return err
	}
	o.Builder = f.NewBuilder
	o.UpdatePodSpecForObject = polymorphichelpers.UpdatePodSpecForObjectFn

	o.DryRunStrategy, err = kcmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}
	kcmdutil.PrintFlagsWithDryRunStrategy(o.PrintFlags, o.DryRunStrategy)
	o.Printer, err = o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}

	if !cmd.Flags().Lookup("initial-delay-seconds").Changed {
		o.InitialDelaySeconds = nil
	}
	if !cmd.Flags().Lookup("timeout-seconds").Changed {
		o.TimeoutSeconds = nil
	}
	if !cmd.Flags().Lookup("period-seconds").Changed {
		o.PeriodSeconds = nil
	}
	if !cmd.Flags().Lookup("success-threshold").Changed {
		o.SuccessThreshold = nil
	}
	if !cmd.Flags().Lookup("failure-threshold").Changed {
		o.FailureThreshold = nil
	}

	clientConfig, err := f.ToRESTConfig()
	if err != nil {
		return err
	}
	o.Client, err = dynamic.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	if len(o.HTTPGet) > 0 {
		url, err := url.Parse(o.HTTPGet)
		if err != nil {
			return fmt.Errorf("--get-url could not be parsed as a valid URL: %v", err)
		}
		var host, port string
		if strings.Contains(url.Host, ":") {
			if host, port, err = net.SplitHostPort(url.Host); err != nil {
				return fmt.Errorf("--get-url did not have a valid port specification: %v", err)
			}
		}
		if host == "localhost" {
			host = ""
		}
		o.HTTPGetAction = &corev1.HTTPGetAction{
			Scheme: corev1.URIScheme(strings.ToUpper(url.Scheme)),
			Host:   host,
			Port:   intOrString(port),
			Path:   url.RequestURI(),
		}
	}

	return nil
}

func (o *ProbeOptions) Validate() error {
	if !o.Readiness && !o.Liveness && !o.Startup {
		return fmt.Errorf("you must specify at least one of --readiness, --liveness, --startup")
	}
	count := 0
	if o.Command != nil {
		count++
	}
	if len(o.OpenTCPSocket) > 0 {
		count++
	}
	if len(o.HTTPGet) > 0 {
		count++
	}

	switch {
	case o.Remove && count != 0:
		return fmt.Errorf("--remove may not be used with any flag except --readiness or --liveness")
	case count > 1:
		return fmt.Errorf("you may only set one of --get-url, --open-tcp, or command")
	case len(o.OpenTCPSocket) > 0 && intOrString(o.OpenTCPSocket).IntVal > 65535:
		return fmt.Errorf("--open-tcp must be a port number between 1 and 65535 or an IANA port name")
	}
	if o.FailureThreshold != nil && *o.FailureThreshold < 1 {
		return fmt.Errorf("--failure-threshold may not be less than one")
	}
	if o.SuccessThreshold != nil && *o.SuccessThreshold < 1 {
		return fmt.Errorf("--success-threshold may not be less than one")
	}
	if o.InitialDelaySeconds != nil && *o.InitialDelaySeconds < 0 {
		return fmt.Errorf("--initial-delay-seconds may not be negative")
	}
	if o.TimeoutSeconds != nil && *o.TimeoutSeconds < 0 {
		return fmt.Errorf("--timeout-seconds may not be negative")
	}
	if o.PeriodSeconds != nil && *o.PeriodSeconds < 0 {
		return fmt.Errorf("--period-seconds may not be negative")
	}
	if len(o.HTTPGet) > 0 && len(o.HTTPGetAction.Port.String()) == 0 {
		return fmt.Errorf("port must be specified as part of a url")
	}

	return nil
}

func (o *ProbeOptions) Run() error {
	b := o.Builder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		LocalParam(o.Local).
		ContinueOnError().
		NamespaceParam(o.Namespace).DefaultNamespace().
		FilenameParam(o.ExplicitNamespace, &o.FilenameOptions).
		Flatten()

	if !o.Local {
		b = b.
			LabelSelectorParam(o.Selector).
			ResourceTypeOrNameArgs(o.All, o.Resources...).
			Latest()
	}

	singleItemImplied := false
	infos, err := b.Do().IntoSingleItemImplied(&singleItemImplied).Infos()
	if err != nil {
		return err
	}

	patches := CalculatePatchesExternal(infos, func(info *resource.Info) (bool, error) {
		transformed := false
		name := getObjectName(info)
		_, err := o.UpdatePodSpecForObject(info.Object, func(spec *corev1.PodSpec) error {
			containers, _ := selectContainers(spec.Containers, o.ContainerSelector)
			if len(containers) == 0 {
				fmt.Fprintf(o.ErrOut, "warning: %s does not have any containers matching %q\n", name, o.ContainerSelector)
				return nil
			}
			// perform updates
			transformed = true
			for _, container := range containers {
				o.updateContainer(container)
			}
			return nil
		})
		return transformed, err
	})
	if singleItemImplied && len(patches) == 0 {
		return fmt.Errorf("%s/%s is not a pod or does not have a pod template", infos[0].Mapping.Resource, infos[0].Name)
	}

	allErrs := []error{}
	for _, patch := range patches {
		info := patch.Info
		name := getObjectName(info)
		if patch.Err != nil {
			allErrs = append(allErrs, fmt.Errorf("error: %s %v\n", name, patch.Err))
			continue
		}

		if string(patch.Patch) == "{}" || len(patch.Patch) == 0 {
			klog.V(1).Infof("info: %s was not changed\n", name)
			continue
		}

		if o.Local || o.DryRunStrategy == kcmdutil.DryRunClient {
			if err := o.Printer.PrintObj(info.Object, o.Out); err != nil {
				allErrs = append(allErrs, err)
			}
			continue
		}

		actual, err := o.Client.Resource(info.Mapping.Resource).Namespace(info.Namespace).Patch(context.TODO(), info.Name, types.StrategicMergePatchType, patch.Patch, metav1.PatchOptions{})
		if err != nil {
			allErrs = append(allErrs, err)
			continue
		}

		if err := o.Printer.PrintObj(actual, o.Out); err != nil {
			allErrs = append(allErrs, err)
		}
	}
	return utilerrors.NewAggregate(allErrs)

}

func (o *ProbeOptions) updateContainer(container *corev1.Container) {
	if o.Remove {
		if o.Readiness {
			container.ReadinessProbe = nil
		}
		if o.Liveness {
			container.LivenessProbe = nil
		}
		if o.Startup {
			container.StartupProbe = nil
		}
		return
	}
	if o.Readiness {
		if container.ReadinessProbe == nil {
			container.ReadinessProbe = &corev1.Probe{}
		}
		o.updateProbe(container.ReadinessProbe)
	}
	if o.Liveness {
		if container.LivenessProbe == nil {
			container.LivenessProbe = &corev1.Probe{}
		}
		o.updateProbe(container.LivenessProbe)
	}
	if o.Startup {
		if container.StartupProbe == nil {
			container.StartupProbe = &corev1.Probe{}
		}
		o.updateProbe(container.StartupProbe)
	}
}

// updateProbe updates only those fields with flags set by the user
func (o *ProbeOptions) updateProbe(probe *corev1.Probe) {
	switch {
	case o.Command != nil:
		probe.Handler = corev1.Handler{Exec: &corev1.ExecAction{Command: o.Command}}
	case o.HTTPGetAction != nil:
		probe.Handler = corev1.Handler{HTTPGet: o.HTTPGetAction}
	case len(o.OpenTCPSocket) > 0:
		probe.Handler = corev1.Handler{TCPSocket: &corev1.TCPSocketAction{Port: intOrString(o.OpenTCPSocket)}}
	}
	if o.InitialDelaySeconds != nil {
		probe.InitialDelaySeconds = int32(*o.InitialDelaySeconds)
	}
	if o.SuccessThreshold != nil {
		probe.SuccessThreshold = int32(*o.SuccessThreshold)
	}
	if o.FailureThreshold != nil {
		probe.FailureThreshold = int32(*o.FailureThreshold)
	}
	if o.TimeoutSeconds != nil {
		probe.TimeoutSeconds = int32(*o.TimeoutSeconds)
	}
	if o.PeriodSeconds != nil {
		probe.PeriodSeconds = int32(*o.PeriodSeconds)
	}
}

func intOrString(s string) intstr.IntOrString {
	if i, err := strconv.Atoi(s); err == nil {
		return intstr.FromInt(i)
	}
	return intstr.FromString(s)
}
