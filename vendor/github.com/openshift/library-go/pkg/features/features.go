package features

import (
	"flag"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	features "github.com/openshift/api/features"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/sets"
	k8sflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/featuregate"
)

type FeatureGateOptions struct {
	featureGateArgs map[string]bool
	featureGates    featuregate.FeatureGate
}

func NewFeatureGateOptions(featureGates featuregate.MutableFeatureGate, profileName features.ClusterProfileName, usedFeatures ...configv1.FeatureGateName) (*FeatureGateOptions, error) {
	err := InitializeFeatureGates(featureGates, profileName, usedFeatures...)
	if err != nil {
		return nil, err
	}
	return &FeatureGateOptions{
		featureGateArgs: map[string]bool{},
		featureGates:    featureGates,
	}, nil
}

func NewFeatureGateOptionsOrDie(featureGates featuregate.MutableFeatureGate, profileName features.ClusterProfileName, usedFeatures ...configv1.FeatureGateName) *FeatureGateOptions {
	ret, err := NewFeatureGateOptions(featureGates, profileName, usedFeatures...)
	if err != nil {
		panic(err)
	}
	return ret
}

func (o *FeatureGateOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.Var(k8sflag.NewMapStringBool(&o.featureGateArgs), "feature-gates", "A set of key=value pairs that describe feature gates for alpha/experimental features. "+
		"Options are:\n"+strings.Join(o.featureGates.KnownFeatures(), "\n"))
}

func (o *FeatureGateOptions) AddFlagsToGoFlagSet(flagset *flag.FlagSet) {
	if flagset == nil {
		flagset = flag.CommandLine
	}

	flagset.Var(k8sflag.NewMapStringBool(&o.featureGateArgs), "feature-gates", "A set of key=value pairs that describe feature gates for alpha/experimental features. "+
		"Options are:\n"+strings.Join(o.featureGates.KnownFeatures(), "\n"))
}

// ApplyTo mutates the featureGates to set the known features and returns a list of printable warnings and an error
// if something fatal happened.
func (o *FeatureGateOptions) ApplyTo(featureGates featuregate.MutableFeatureGate) ([]string, error) {
	return setFeatureGates(o.featureGateArgs, featureGates)
}

// SetFeatureGates sets the featuregates from the flags and return a list of printable warnings and an error
// if there was a failure.  If you already have the Map from the CLI version, use featureGates.SetFromMap.
func SetFeatureGates(flags map[string][]string, featureGates featuregate.MutableFeatureGate) ([]string, error) {
	featureGatesMap := map[string]bool{}
	featureGateParser := k8sflag.NewMapStringBool(&featureGatesMap)
	for _, val := range flags["feature-gates"] {
		if err := featureGateParser.Set(val); err != nil {
			return []string{}, err
		}
	}

	return setFeatureGates(featureGatesMap, featureGates)
}

func setFeatureGates(featureGatesMap map[string]bool, featureGates featuregate.MutableFeatureGate) ([]string, error) {
	warnings := []string{}

	// filter to only the known featuregates because OCP specifies lots of features that only for certain components.
	// ideally we filter these at the operator level, but that isn't trivial to do and this is.
	// We don't allow users to set values, so hopefully we have e2e test that prevent invalid values.
	allowedFeatureGates := map[string]bool{}
	knownFeatures := featureGates.GetAll()
	for featureGateName, val := range featureGatesMap {
		if _, exists := knownFeatures[featuregate.Feature(featureGateName)]; !exists {
			warnings = append(warnings, fmt.Sprintf("Ignoring unknown FeatureGate %q", featureGateName))
			continue
		}
		allowedFeatureGates[featureGateName] = val
	}

	if err := featureGates.SetFromMap(allowedFeatureGates); err != nil {
		return warnings, err
	}

	return warnings, nil
}

// InitializeFeatureGates should be called when your binary is starting with your featuregate instance and the list of
// featuregates that your process will honor.
func InitializeFeatureGates(featureGates featuregate.MutableFeatureGate, profileName features.ClusterProfileName, usedFeatures ...configv1.FeatureGateName) error {
	defaultFeatures := sets.Set[string]{}
	enabledDefaultFeatures := sets.Set[string]{}
	allFeatureSets := features.AllFeatureSets()[profileName]
	for _, enabled := range allFeatureSets[configv1.Default].Enabled {
		defaultFeatures.Insert(string(enabled.FeatureGateAttributes.Name))
		enabledDefaultFeatures.Insert(string(enabled.FeatureGateAttributes.Name))
	}
	for _, disabled := range allFeatureSets[configv1.Default].Disabled {
		defaultFeatures.Insert(string(disabled.FeatureGateAttributes.Name))
	}

	localFeatures := map[featuregate.Feature]featuregate.FeatureSpec{}
	for _, featureName := range usedFeatures {
		if enabledDefaultFeatures.Has(string(featureName)) {
			localFeatures[featuregate.Feature(featureName)] = featuregate.FeatureSpec{
				Default:    true,
				PreRelease: featuregate.GA,
			}
			continue
		}
		localFeatures[featuregate.Feature(featureName)] = featuregate.FeatureSpec{
			Default:    false,
			PreRelease: featuregate.Alpha,
		}
	}

	if err := featureGates.Add(localFeatures); err != nil {
		return err
	}

	return nil
}
