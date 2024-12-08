package commatrix

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	log "github.com/sirupsen/logrus"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/openshift-kni/commatrix/pkg/client"
	commatrixcreator "github.com/openshift-kni/commatrix/pkg/commatrix-creator"
	"github.com/openshift-kni/commatrix/pkg/endpointslices"
	"github.com/openshift-kni/commatrix/pkg/types"
	"github.com/openshift-kni/commatrix/pkg/utils"
)

var (
	commatrixLong = templates.LongDesc(`
		Generate the communication matrix

		This command to generate the communication matrix on nodes`)
	CommatrixExample = templates.Examples(`
		oc adm commatrix generate
	`)
)

type CommatrixOptions struct {
	destDir             string
	format              string
	customEntriesPath   string
	customEntriesFormat string
	debug               bool
	genericiooptions.IOStreams
}

func Newcommatrix(streams genericiooptions.IOStreams) *CommatrixOptions {
	return &CommatrixOptions{
		IOStreams: streams,
	}
}

// NewCmdCommatrix
func NewCmdCommatrix(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "commatrix",
		Short: "Generate the communication matrix",
		Long:  commatrixLong,
		Run:   kcmdutil.DefaultSubCommandRun(streams.ErrOut),
	}
	cmds.AddCommand(NewCmdCommatrixGenerate(f, streams))

	return cmds
}

// NewCmdAddRoleToUser implements the OpenShift cli add-role-to-user command
func NewCmdCommatrixGenerate(f kcmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := Newcommatrix(streams)
	cmd := &cobra.Command{
		Use:     "generate",
		Short:   "Generate the communication matrix",
		Long:    commatrixLong,
		Example: CommatrixExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.RunCommatrixGenerate())
		},
	}
	cmd.Flags().StringVar(&o.destDir, "destDir", "communication-matrix", "Output files dir")
	cmd.Flags().StringVar(&o.format, "format", "csv", "Desired format (json,yaml,csv,nft)")
	cmd.Flags().StringVar(&o.customEntriesPath, "customEntriesPath", "", "Add custom entries from a file to the matrix")
	cmd.Flags().StringVar(&o.customEntriesFormat, "customEntriesFormat", "", "Set the format of the custom entries file (json,yaml,csv)")
	cmd.Flags().BoolVar(&o.debug, "debug", false, "Debug logs")

	return cmd
}

// Complete initializes the options based on the provided arguments and flags
func (o *CommatrixOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	// Validate the number of arguments
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}

	// Initialize any dependencies or derived fields if needed
	if o.destDir == "" {
		o.destDir = "communication-matrix" // Default value
	}

	if o.format == "" {
		o.format = "csv" // Default format
	}

	if o.customEntriesPath != "" && o.customEntriesFormat == "" {
		return fmt.Errorf("you must specify the --customEntriesFormat when using --customEntriesPath")
	}

	return nil
}

func (o *CommatrixOptions) Validate() error {
	// Validate destination directory
	if o.destDir == "" {
		return fmt.Errorf("destination directory cannot be empty")
	}

	// Validate format
	validFormats := map[string]bool{"csv": true, "json": true, "yaml": true, "nft": true}
	if _, valid := validFormats[o.format]; !valid {
		return fmt.Errorf("invalid format '%s', valid options are: csv, json, yaml, nft", o.format)
	}

	// Validate custom entries path and format
	if o.customEntriesPath != "" {
		if o.customEntriesFormat == "" {
			return fmt.Errorf("you must specify the --customEntriesFormat when using --customEntriesPath")
		}

		validCustomFormats := map[string]bool{"csv": true, "json": true, "yaml": true}
		if _, valid := validCustomFormats[o.customEntriesFormat]; !valid {
			return fmt.Errorf("invalid custom entries format '%s', valid options are: csv, json, yaml", o.customEntriesFormat)
		}
	}

	return nil
}

func (o *CommatrixOptions) RunCommatrixGenerate() error {
	if o.debug {
		log.SetLevel(log.DebugLevel)
	}

	cs, err := client.New()
	handleError("Failed creating the k8s client", err)

	utilsHelpers := utils.New(cs)
	log.Debug("Utils helpers initialized")

	deployment, infra, err := detectDeploymentAndInfra(utilsHelpers)
	if err != nil {
		return err
	}

	epExporter, err := endpointslices.New(cs)
	if err != nil {
		return fmt.Errorf("failed creating the endpointslices exporter %s", err)
	}

	matrix, err := generateAndWriteCommunicationMatrix(epExporter, o.destDir, deployment, infra, o.customEntriesPath, o.customEntriesFormat)
	if err != nil {
		return err
	}
	res, err := matrix.Print(o.format)
	if err != nil {
		return err
	}

	fmt.Println(string(res))

	return nil
}

func handleError(message string, err error) {
	if err != nil {
		log.Panicf("%s: %v", message, err)
	}
}

func detectDeploymentAndInfra(utilsHelpers utils.UtilsInterface) (types.Deployment, types.Env, error) {
	log.Debug("Detecting deployment and infra types")

	deployment := types.Standard
	isSNO, err := utilsHelpers.IsSNOCluster()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to detect deployment type %s", err)
	}

	if isSNO {
		deployment = types.SNO
	}

	infra := types.Cloud
	isBM, err := utilsHelpers.IsBMInfra()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to detect infra type %s", err)
	}
	if isBM {
		infra = types.Baremetal
	}

	return deployment, infra, err
}

func generateAndWriteCommunicationMatrix(epExporter *endpointslices.EndpointSlicesExporter, destDir string, deployment types.Deployment, infra types.Env, customEntriesPath, customEntriesFormat string) (*types.ComMatrix, error) {
	log.Debug("Creating communication matrix")
	createNestedDirectory(destDir)
	commMatrix, err := commatrixcreator.New(epExporter, customEntriesPath, customEntriesFormat, infra, deployment)
	if err != nil {
		return nil, err
	}

	matrix, err := commMatrix.CreateEndpointMatrix()
	if err != nil {
		return nil, err
	}

	return matrix, nil
}

func createNestedDirectory(destDir string) error {
	// Create nested directory structure
	nestedDir := filepath.Join(destDir, "communication-matrix")
	err := os.MkdirAll(nestedDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create nested directory %s: %v", nestedDir, err)
	}
	log.Printf("Successfully created nested directory: %s", nestedDir)
	return nil
}
