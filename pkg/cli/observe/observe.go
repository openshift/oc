package observe

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift/oc/pkg/helpers/proc"
)

var (
	observeLong = templates.LongDesc(`
		Observe changes to resources and take action on them

		This command assists in building scripted reactions to changes that occur in
		Kubernetes or OpenShift resources. This is frequently referred to as a
		'controller' in Kubernetes and acts to ensure particular conditions are
		maintained. On startup, observe will list all of the resources of a
		particular type and execute the provided script on each one. Observe watches
		the server for changes, and will reexecute the script for each update.

		Observe works best for problems of the form "for every resource X, make sure
		Y is true". Some examples of ways observe can be used include:

		* Ensure every namespace has a quota or limit range object
		* Ensure every service is registered in DNS by making calls to a DNS API
		* Send an email alert whenever a node reports 'NotReady'
		* Watch for the 'FailedScheduling' event and write an IRC message
		* Dynamically provision persistent volumes when a new PVC is created
		* Delete pods that have reached successful completion after a period of time.

		The simplest pattern is maintaining an invariant on an object - for instance,
		"every namespace should have an annotation that indicates its owner". If the
		object is deleted no reaction is necessary. A variation on that pattern is
		creating another object: "every namespace should have a quota object based
		on the resources allowed for an owner".

		    $ cat set_owner.sh
		    #!/bin/sh
		    if [[ "$(arvan paas get namespace "$1" --template='{{ .metadata.annotations.owner }}')" == "" ]]; then
		      arvan paas annotate namespace "$1" owner=bob
		    fi

		    $ arvan paas observe namespaces -- ./set_owner.sh

		The set_owner.sh script is invoked with a single argument (the namespace name)
		for each namespace. This simple script ensures that any user without the
		"owner" annotation gets one set, but preserves any existing value.

		The next common of controller pattern is provisioning - making changes in an
		external system to match the state of a Kubernetes resource. These scripts need
		to account for deletions that may take place while the observe command is not
		running. You can provide the list of known objects via the --names command,
		which should return a newline-delimited list of names or namespace/name pairs.
		Your command will be invoked whenever observe checks the latest state on the
		server - any resources returned by --names that are not found on the server
		will be passed to your --delete command.

		For example, you may wish to ensure that every node that is added to Kubernetes
		is added to your cluster inventory along with its IP:

		    $ cat add_to_inventory.sh
		    #!/bin/sh
		    echo "$1 $2" >> inventory
		    sort -u inventory -o inventory

		    $ cat remove_from_inventory.sh
		    #!/bin/sh
		    grep -vE "^$1 " inventory > /tmp/newinventory
		    mv -f /tmp/newinventory inventory

		    $ cat known_nodes.sh
		    #!/bin/sh
		    touch inventory
		    cut -f 1-1 -d ' ' inventory

		    $ arvan paas observe nodes --template '{ .status.addresses[0].address }' \
		      --names ./known_nodes.sh \
		      --delete ./remove_from_inventory.sh \
		      -- ./add_to_inventory.sh

		If you stop the observe command and then delete a node, when you launch observe
		again the contents of inventory will be compared to the list of nodes from the
		server, and any node in the inventory file that no longer exists will trigger
		a call to remove_from_inventory.sh with the name of the node.

		Important: when handling deletes, the previous state of the object may not be
		available and only the name/namespace of the object will be passed to	your
		--delete command as arguments (all custom arguments are omitted).

		More complicated interactions build on the two examples above - your inventory
		script could make a call to allocate storage on your infrastructure as a
		service, or register node names in DNS, or set complex firewalls. The more
		complex your integration, the more important it is to record enough data in the
		remote system that you can identify when resources on either side are deleted.\
	`)

	observeExample = templates.Examples(`
		# Observe changes to services
		arvan paas observe services

		# Observe changes to services, including the clusterIP and invoke a script for each
		arvan paas observe services --template '{ .spec.clusterIP }' -- register_dns.sh

		# Observe changes to services filtered by a label selector
		arvan paas observe namespaces -l regist-dns=true --template '{ .spec.clusterIP }' -- register_dns.sh
	`)
)

type ObserveOptions struct {
	PrintFlags *genericclioptions.PrintFlags

	debugOut io.Writer
	quiet    bool

	client           resource.RESTClient
	mapping          *meta.RESTMapping
	includeNamespace bool

	// which resources to select
	namespace     string
	allNamespaces bool
	selector      string

	// additional debugging information
	listenAddr string

	// actions to take on each object
	eachCommand   []string
	objectEnvVar  string
	typeEnvVar    string
	deleteCommand stringSliceFlag

	// controls whether deletes are included
	nameSyncCommand stringSliceFlag

	// error handling logic
	observedErrors  int
	maximumErrors   int
	retryCount      int
	retryExitStatus int

	// when to exit or reprocess the list of items
	once               bool
	exitAfterPeriod    time.Duration
	resyncPeriod       time.Duration
	printMetricsOnExit bool

	// control the output of the command
	templates       stringSliceFlag
	printer         printerWrapper
	strictTemplates bool

	argumentStore *objectArgumentsStore
	// knownObjects is nil if we do not need to track deletions
	knownObjects knownObjects

	genericclioptions.IOStreams
}

func NewObserveOptions(streams genericclioptions.IOStreams) *ObserveOptions {
	return &ObserveOptions{
		PrintFlags: (&genericclioptions.PrintFlags{
			TemplatePrinterFlags: genericclioptions.NewKubeTemplatePrintFlags(),
		}).WithDefaultOutput("jsonpath").WithTypeSetter(scheme.Scheme),
		IOStreams: streams,

		retryCount:    2,
		maximumErrors: 20,
		listenAddr:    ":11251",
	}
}

// NewCmdObserve creates the observe command.
func NewCmdObserve(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewObserveOptions(streams)

	cmd := &cobra.Command{
		Use:     "observe RESOURCE [-- COMMAND ...]",
		Short:   "Observe changes to resources and react to them (experimental)",
		Long:    observeLong,
		Example: observeExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}

	// flags controlling what to select
	cmd.Flags().BoolVarP(&o.allNamespaces, "all-namespaces", "A", o.allNamespaces, "If true, list the requested object(s) across all projects. Project in current context is ignored.")
	cmd.Flags().StringVarP(&o.selector, "selector", "l", o.selector, "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")

	// to perform deletion synchronization
	cmd.Flags().VarP(&o.deleteCommand, "delete", "d", "A command to run when resources are deleted. Specify multiple times to add arguments.")
	cmd.Flags().Var(&o.nameSyncCommand, "names", "A command that will list all of the currently known names, optional. Specify multiple times to add arguments. Use to get notifications when objects are deleted.")

	// add additional arguments / info to the server
	cmd.Flags().BoolVar(&o.strictTemplates, "strict-templates", o.strictTemplates, "If true return an error on any field or map key that is not missing in a template.")
	cmd.Flags().MarkDeprecated("strict-templates", "and will be removed in a future release. Use --allow-missing-template-keys=false instead.")
	cmd.Flags().VarP(&o.templates, "argument", "a", "Template for the arguments to be passed to each command in the format defined by --output.")
	cmd.Flags().MarkShorthandDeprecated("a", "and will be removed in a future release. Use --template instead.")
	cmd.Flags().MarkDeprecated("argument", "and will be removed in a future release. Use --template instead.")
	cmd.Flags().StringVar(&o.typeEnvVar, "type-env-var", o.typeEnvVar, "The name of an env var to set with the type of event received ('Sync', 'Updated', 'Deleted', 'Added') to the reaction command or --delete.")
	cmd.Flags().StringVar(&o.objectEnvVar, "object-env-var", o.objectEnvVar, "The name of an env var to serialize the object to when calling the command, optional.")

	// control retries of individual commands
	cmd.Flags().IntVar(&o.maximumErrors, "maximum-errors", o.maximumErrors, "Exit after this many errors have been detected with. May be set to -1 for no maximum.")
	cmd.Flags().IntVar(&o.retryExitStatus, "retry-on-exit-code", o.retryExitStatus, "If any command returns this exit code, retry up to --retry-count times.")
	cmd.Flags().IntVar(&o.retryCount, "retry-count", o.retryCount, "The number of times to retry a failing command before continuing.")

	// control observe program behavior
	cmd.Flags().BoolVar(&o.once, "once", o.once, "If true, exit with a status code 0 after all current objects have been processed.")
	cmd.Flags().DurationVar(&o.exitAfterPeriod, "exit-after", o.exitAfterPeriod, "Exit with status code 0 after the provided duration, optional.")
	cmd.Flags().DurationVar(&o.resyncPeriod, "resync-period", o.resyncPeriod, "When non-zero, periodically reprocess every item from the server as a Sync event. Use to ensure external systems are kept up to date.")
	cmd.Flags().BoolVar(&o.printMetricsOnExit, "print-metrics-on-exit", o.printMetricsOnExit, "If true, on exit write all metrics to stdout.")
	cmd.Flags().StringVar(&o.listenAddr, "listen-addr", o.listenAddr, "The name of an interface to listen on to expose metrics and health checking.")

	// additional debug output
	cmd.Flags().BoolVar(&o.quiet, "no-headers", o.quiet, "If true, skip printing information about each event prior to executing the command.")
	cmd.Flags().MarkDeprecated("no-headers", "and will be removed in a future release. Use --quiet instead.")
	cmd.Flags().BoolVarP(&o.quiet, "quiet", "q", o.quiet, "If true, skip printing information about each event prior to executing the command.")

	o.PrintFlags.AddFlags(cmd)
	return cmd
}

func (o *ObserveOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	var err error

	var command []string
	if i := cmd.ArgsLenAtDash(); i != -1 {
		command = args[i:]
		args = args[:i]
	}

	o.eachCommand = command

	switch len(args) {
	case 0:
		return fmt.Errorf("you must specify at least one argument containing the resource to observe")
	case 1:
	default:
		return fmt.Errorf("you may only specify one argument containing the resource to observe (use '--' to separate your resource and your command)")
	}

	gr := schema.ParseGroupResource(args[0])
	if gr.Empty() {
		return fmt.Errorf("unknown resource argument")
	}

	mapper, err := f.ToRESTMapper()
	if err != nil {
		return err
	}

	version, err := mapper.KindFor(gr.WithVersion(""))
	if err != nil {
		return err
	}
	mapping, err := mapper.RESTMapping(version.GroupKind())
	if err != nil {
		return err
	}
	o.mapping = mapping
	o.includeNamespace = mapping.Scope.Name() == meta.RESTScopeNamespace.Name()

	o.client, err = f.UnstructuredClientForMapping(mapping)
	if err != nil {
		return err
	}

	o.namespace, _, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	// TODO: Remove in the next release
	// support backwards compatibility with misspelling of "go-template" output format
	if o.PrintFlags.OutputFormat != nil && *o.PrintFlags.OutputFormat == "gotemplate" {
		fmt.Fprintf(o.ErrOut, "DEPRECATED: The 'gotemplate' output format has been replaced by 'go-template', please use the new spelling instead\n")
		*o.PrintFlags.OutputFormat = "go-template"
	}

	// TODO: Remove in the next release
	// support backwards compatibility with incorrect flag --strict-templates
	if o.strictTemplates {
		*o.PrintFlags.TemplatePrinterFlags.AllowMissingKeys = !o.strictTemplates
	}

	// TODO: Remove in the next rlease
	// support backwards compatibility with incorrect flag --argument
	templatesStr := o.templates.String()
	if len(templatesStr) > 0 {
		*o.PrintFlags.TemplatePrinterFlags.TemplateArgument = templatesStr
	}

	// prevent output-format from defaulting to go-template if no --output format
	// is explicitly provided by the user, which was added as backwards
	// compatibility see:
	// https://github.com/kubernetes/cli-runtime/blob/3002134656c683c343e3688a8c95df5bfcd9c458/pkg/genericclioptions/print_flags.go#L89
	o.PrintFlags.OutputFlagSpecified = func() bool { return true }

	var printer printers.ResourcePrinter
	if o.PrintFlags.TemplatePrinterFlags.TemplateArgument != nil && len(*o.PrintFlags.TemplatePrinterFlags.TemplateArgument) > 0 {
		printer, err = o.PrintFlags.ToPrinter()
		if err != nil {
			return err
		}
	} else {
		printer = printers.NewDiscardingPrinter()
	}
	o.printer = printerWrapper{printer: printer}

	if o.quiet {
		o.debugOut = ioutil.Discard
	} else {
		o.debugOut = o.Out
	}

	o.argumentStore = &objectArgumentsStore{}
	switch {
	case len(o.nameSyncCommand) > 0:
		o.argumentStore.keyFn = func() ([]string, error) {
			var out []byte
			err := retryCommandError(o.retryExitStatus, o.retryCount, func() error {
				c := exec.Command(o.nameSyncCommand[0], o.nameSyncCommand[1:]...)
				var err error
				return measureCommandDuration(nameExecDurations, func() error {
					out, err = c.Output()
					return err
				})
			})
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					if len(exit.Stderr) > 0 {
						err = fmt.Errorf("%v\n%s", err, string(exit.Stderr))
					}
				}
				return nil, err
			}
			names := strings.Split(string(out), "\n")
			sort.Sort(sort.StringSlice(names))
			var outputNames []string
			for i, s := range names {
				if len(s) != 0 {
					outputNames = names[i:]
					break
				}
			}
			klog.V(4).Infof("Found existing keys: %v", outputNames)
			return outputNames, nil
		}
		o.knownObjects = o.argumentStore
	case len(o.deleteCommand) > 0, o.resyncPeriod > 0:
		o.knownObjects = o.argumentStore
	}

	return nil
}

func (o *ObserveOptions) Validate() error {
	if len(o.nameSyncCommand) > 0 && len(o.deleteCommand) == 0 {
		return fmt.Errorf("--delete and --names must both be specified")
	}

	if _, err := labels.Parse(o.selector); err != nil {
		return err
	}

	return nil
}

func (o *ObserveOptions) Run() error {
	if len(o.deleteCommand) > 0 && len(o.nameSyncCommand) == 0 {
		fmt.Fprintf(o.ErrOut, "warning: If you are modifying resources outside of %q, you should use the --names command to ensure you don't miss deletions that occur while the command is not running.\n", o.mapping.Resource)
	}

	// watch the given resource for changes
	store := cache.NewDeltaFIFO(objectArgumentsKeyFunc, o.knownObjects)
	lw := restListWatcher{
		Helper:   resource.NewHelper(o.client, o.mapping),
		selector: o.selector,
	}
	if !o.allNamespaces {
		lw.namespace = o.namespace
	}

	// ensure any child processes are reaped if we are running as PID 1
	proc.StartReaper()

	// listen on the provided address for metrics
	if len(o.listenAddr) > 0 {
		prometheus.MustRegister(observeCounts)
		prometheus.MustRegister(execDurations)
		prometheus.MustRegister(nameExecDurations)
		errWaitingForSync := fmt.Errorf("waiting for initial sync")
		healthz.InstallHandler(http.DefaultServeMux, healthz.NamedCheck("ready", func(r *http.Request) error {
			if !store.HasSynced() {
				return errWaitingForSync
			}
			return nil
		}))
		http.Handle("/metrics", promhttp.Handler())
		go func() {
			klog.Fatalf("Unable to listen on %q: %v", o.listenAddr, http.ListenAndServe(o.listenAddr, nil))
		}()
		klog.V(2).Infof("Listening on %s at /metrics and /healthz", o.listenAddr)
	}

	// exit cleanly after a certain period
	// lock guards the loop to ensure no child processes are running
	var lock sync.Mutex
	if o.exitAfterPeriod > 0 {
		go func() {
			<-time.After(o.exitAfterPeriod)
			lock.Lock()
			defer lock.Unlock()
			o.dumpMetrics()
			fmt.Fprintf(o.ErrOut, "Shutting down after %s ...\n", o.exitAfterPeriod)
			os.Exit(0)
		}()
	}

	defer o.dumpMetrics()
	stopCh := make(chan struct{})
	defer close(stopCh)

	// start the reflector
	reflector := cache.NewNamedReflector("observer", lw, nil, store, o.resyncPeriod)
	go func() {
		observedListErrors := 0
		wait.Until(func() {
			if err := reflector.ListAndWatch(stopCh); err != nil {
				utilruntime.HandleError(err)
				observedListErrors++
				if o.maximumErrors != -1 && observedListErrors > o.maximumErrors {
					klog.Fatalf("Maximum list errors of %d reached, exiting", o.maximumErrors)
				}
			}
		}, time.Second, stopCh)
	}()

	if o.once {
		// wait until the reflector reports it has completed the initial list and the
		// fifo has been populated
		for len(reflector.LastSyncResourceVersion()) == 0 {
			time.Sleep(50 * time.Millisecond)
		}
		// if the store is empty, there is nothing to sync
		if store.HasSynced() && len(store.ListKeys()) == 0 {
			fmt.Fprintf(o.ErrOut, "Nothing to sync, exiting immediately\n")
			return nil
		}
	}

	// process all changes that occur in the resource
	syncing := false
	for {
		_, err := store.Pop(func(obj interface{}) error {
			// if we failed to retrieve the list of keys, exit
			if err := o.argumentStore.ListKeysError(); err != nil {
				return fmt.Errorf("unable to list known keys: %v", err)
			}

			deltas := obj.(cache.Deltas)
			for _, delta := range deltas {
				if err := func() error {
					lock.Lock()
					defer lock.Unlock()

					// handle before and after observe notification
					switch {
					case !syncing && delta.Type == cache.Sync:
						if err := o.startSync(); err != nil {
							return err
						}
						syncing = true
					case syncing && delta.Type != cache.Sync:
						if err := o.finishSync(); err != nil {
							return err
						}
						syncing = false
					}

					// require the user to provide a name function in order to get events beyond added / updated
					if !syncing && o.knownObjects == nil && !(delta.Type == cache.Added || delta.Type == cache.Updated) {
						return nil
					}

					observeCounts.WithLabelValues(string(delta.Type)).Inc()

					// calculate the arguments for the delta and then invoke any command
					object, arguments, output, err := o.calculateArguments(delta)
					if err != nil {
						return err
					}
					if err := o.next(delta.Type, object, output, arguments); err != nil {
						return err
					}

					return nil
				}(); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}

		// if we only want to run once, exit here
		if o.once && store.HasSynced() {
			if syncing {
				if err := o.finishSync(); err != nil {
					return err
				}
			}
			return nil
		}
	}
}

// calculateArguments determines the arguments for a give delta and updates the argument store, or returns
// an error. If the object can be transformed into a full JSON object, that is also returned.
func (o *ObserveOptions) calculateArguments(delta cache.Delta) (runtime.Object, []string, []byte, error) {
	var arguments []string
	var object runtime.Object
	var key string
	var output []byte

	switch t := delta.Object.(type) {
	case cache.DeletedFinalStateUnknown:
		key = t.Key
		if obj, ok := t.Obj.(runtime.Object); ok {
			object = obj

			args, data, err := o.printer.PrintObj(object)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("unable to write arguments: %v", err)
			}
			arguments = args
			output = data

		} else {
			value, _, err := o.argumentStore.GetByKey(key)
			if err != nil {
				return nil, nil, nil, err
			}
			if value != nil {
				args, ok := value.(objectArguments)
				if !ok {
					return nil, nil, nil, fmt.Errorf("unexpected cache value %T", value)
				}
				arguments = args.arguments
				output = args.output
			}
		}

		o.argumentStore.Remove(key)

	case runtime.Object:
		object = t

		args, data, err := o.printer.PrintObj(object)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("unable to write arguments: %v", err)
		}
		arguments = args
		output = data

		key, _ = cache.MetaNamespaceKeyFunc(t)
		if delta.Type == cache.Deleted {
			o.argumentStore.Remove(key)
		} else {
			saved := objectArguments{key: key, arguments: arguments}
			// only cache the object data if the commands will be using it.
			if len(o.objectEnvVar) > 0 {
				saved.output = output
			}
			o.argumentStore.Put(key, saved)
		}

	case objectArguments:
		key = t.key
		arguments = t.arguments
		output = t.output

	default:
		return nil, nil, nil, fmt.Errorf("unrecognized object %T from cache store", delta.Object)
	}

	if object == nil {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return nil, nil, nil, err
		}
		unstructured := &unstructured.Unstructured{}
		unstructured.SetNamespace(namespace)
		unstructured.SetName(name)
		object = unstructured
	}

	return object, arguments, output, nil
}

func (o *ObserveOptions) startSync() error {
	fmt.Fprintf(o.debugOut, "# %s Sync started\n", time.Now().Format(time.RFC3339))
	return nil
}
func (o *ObserveOptions) finishSync() error {
	fmt.Fprintf(o.debugOut, "# %s Sync ended\n", time.Now().Format(time.RFC3339))
	return nil
}

func (o *ObserveOptions) next(deltaType cache.DeltaType, obj runtime.Object, output []byte, arguments []string) error {
	klog.V(4).Infof("Processing %s %v: %#v", deltaType, arguments, obj)
	m, err := meta.Accessor(obj)
	if err != nil {
		return fmt.Errorf("unable to handle %T: %v", obj, err)
	}
	resourceVersion := m.GetResourceVersion()

	outType := string(deltaType)

	var args []string
	if o.includeNamespace {
		args = append(args, m.GetNamespace())
	}
	args = append(args, m.GetName())

	var command string
	switch {
	case deltaType == cache.Deleted:
		if len(o.deleteCommand) > 0 {
			command = o.deleteCommand[0]
			args = append(append([]string{}, o.deleteCommand[1:]...), args...)
		}
	case len(o.eachCommand) > 0:
		command = o.eachCommand[0]
		args = append(append([]string{}, o.eachCommand[1:]...), args...)
	}

	args = append(args, arguments...)

	if len(command) == 0 {
		fmt.Fprintf(o.debugOut, "# %s %s %s\t%s\n", time.Now().Format(time.RFC3339), outType, resourceVersion, printCommandLine(command, args...))
		return nil
	}

	fmt.Fprintf(o.debugOut, "# %s %s %s\t%s\n", time.Now().Format(time.RFC3339), outType, resourceVersion, printCommandLine(command, args...))

	out, errOut := &newlineTrailingWriter{w: o.Out}, &newlineTrailingWriter{w: o.ErrOut}

	err = retryCommandError(o.retryExitStatus, o.retryCount, func() error {
		cmd := exec.Command(command, args...)
		cmd.Stdout = out
		cmd.Stderr = errOut
		cmd.Env = os.Environ()
		if len(o.objectEnvVar) > 0 {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", o.objectEnvVar, string(output)))
		}
		if len(o.typeEnvVar) > 0 {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", o.typeEnvVar, outType))
		}
		err := measureCommandDuration(execDurations, cmd.Run, outType)
		out.Flush()
		errOut.Flush()
		return err
	})
	if err != nil {
		if code, ok := exitCodeForCommandError(err); ok && code != 0 {
			err = fmt.Errorf("command %q exited with status code %d", command, code)
		}
		return o.handleCommandError(err)
	}
	return nil
}

func (o *ObserveOptions) handleCommandError(err error) error {
	if err == nil {
		return nil
	}
	o.observedErrors++
	fmt.Fprintf(o.ErrOut, "error: %v\n", err)
	if o.maximumErrors == -1 || o.observedErrors < o.maximumErrors {
		return nil
	}
	return fmt.Errorf("reached maximum error limit of %d, exiting", o.maximumErrors)
}

func (o *ObserveOptions) dumpMetrics() {
	if !o.printMetricsOnExit {
		return
	}
	w := httptest.NewRecorder()
	promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{}).ServeHTTP(w, &http.Request{})
	if w.Code == http.StatusOK {
		fmt.Fprintf(o.Out, w.Body.String())
	}
}

func printCommandLine(cmd string, args ...string) string {
	outCmd := cmd
	if strings.ContainsAny(outCmd, "\"\\ ") {
		outCmd = strconv.Quote(outCmd)
	}
	if len(outCmd) == 0 {
		outCmd = "\"\""
	}
	outArgs := make([]string, 0, len(args))
	for _, s := range args {
		if strings.ContainsAny(s, "\"\\ ") {
			s = strconv.Quote(s)
		}
		if len(s) == 0 {
			s = "\"\""
		}
		outArgs = append(outArgs, s)
	}
	if len(outArgs) == 0 {
		return outCmd
	}
	return fmt.Sprintf("%s %s", outCmd, strings.Join(outArgs, " "))
}

type stringSliceFlag []string

func (f *stringSliceFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func (f *stringSliceFlag) String() string { return strings.Join(*f, " ") }
func (f *stringSliceFlag) Type() string   { return "string" }
