package appregistry

import (
	"strings"
)

// OperatorSourceSpecifier interface provides capability to have different ways
// of specifying operator source via command line flags. This helps us
// maintain backward compatability.
type OperatorSourceSpecifier interface {
	Parse(specifiers []string) ([]*Source, error)
}

type Input struct {
	// Sources is the set of remote operator source(s) specified where operator
	// manifest(s) are located.
	Sources []*Source

	// Packages is the set of package name(s) specified.
	Packages []string
}

func (i *Input) IsGoodToProceed() bool {
	return len(i.Sources) > 0 && len(i.Packages) > 0
}

func (i *Input) PackagesToMap() map[string]bool {
	packages := map[string]bool{}

	for _, pkg := range i.Packages {
		packages[pkg] = false
	}

	return packages
}

type inputParser struct {
	sourceSpecifier OperatorSourceSpecifier
}

// Parse parses the raw input provided, sanitizes it and returns an instance of
// Input.
//
// csvSources is a slice of operator source(s) specified. Each operator source
// is expected to be specified as follows.
//
// {base url with cnr prefix}|{quay registry namespace}|{secret namespace/secret name}
//
// csvPackages is a comma separated list of package(s). It is expected to have
// the following format.
// etcd,prometheus,descheduler
//
func (p *inputParser) Parse(csvSources []string, csvPackages string) (*Input, error) {
	sources, err := p.sourceSpecifier.Parse(csvSources)
	if err != nil && len(sources) == 0 {
		return nil, err
	}

	packages := sanitizePackageList(strings.Split(csvPackages, ","))

	return &Input{
		Sources:  sources,
		Packages: packages,
	}, err
}

// sanitizePackageList sanitizes the set of package(s) specified. It removes
// duplicates and ignores empty string.
func sanitizePackageList(in []string) []string {
	out := make([]string, 0)

	inMap := map[string]bool{}
	for _, item := range in {
		if _, ok := inMap[item]; ok || item == "" {
			continue
		}

		inMap[item] = true
		out = append(out, item)
	}

	return out
}
