package catalog

import (
	"fmt"
	"path/filepath"

	"github.com/openshift/oc/pkg/cli/image/imagesource"
)

type IndexImageMirrorerOptions struct {
	ImageMirrorer     ImageMirrorer
	DatabaseExtractor DatabaseExtractor

	Source, Dest      imagesource.TypedImageReference
	ManifestDir       string
	MaxPathComponents int
}

func (o *IndexImageMirrorerOptions) Validate() error {
	if o.ImageMirrorer == nil {
		return fmt.Errorf("can't mirror without a mirrorer configured")
	}
	if o.DatabaseExtractor == nil {
		return fmt.Errorf("can't mirror without a database extractor configured")
	}
	if o.Source.String() == "" {
		return fmt.Errorf("source image required")
	}

	if o.Dest.Ref.RegistryURL().Hostname() == "" {
		return fmt.Errorf("destination registry required")
	}

	if o.ManifestDir == "" {
		return fmt.Errorf("must have directory to write manifests to")
	}
	if o.MaxPathComponents < 0 {
		return fmt.Errorf("max path components must be a positive integer, or 0 for no limit")
	}
	return nil
}

func (o *IndexImageMirrorerOptions) Complete() error {
	if o.ManifestDir == "" {
		o.ManifestDir = filepath.Join(".", "manifests")
	}
	return nil
}

// Apply sequentially applies the given options to the config.
func (c *IndexImageMirrorerOptions) Apply(options []ImageIndexMirrorOption) {
	for _, option := range options {
		option(c)
	}
}

// ToOption converts an IndexImageMirrorerOptions object into a function that applies
// its current configuration to another IndexImageMirrorerOptions instance
func (c *IndexImageMirrorerOptions) ToOption() ImageIndexMirrorOption {
	return func(o *IndexImageMirrorerOptions) {
		if c.ImageMirrorer != nil {
			o.ImageMirrorer = c.ImageMirrorer
		}
		if c.DatabaseExtractor != nil {
			o.DatabaseExtractor = c.DatabaseExtractor
		}
		if len(c.Source.String()) != 0 {
			o.Source = c.Source
		}
		if len(c.Dest.String()) != 0 {
			o.Dest = c.Dest
		}
		if len(c.ManifestDir) != 0 {
			o.ManifestDir = c.ManifestDir
		}
		if c.MaxPathComponents > 0 {
			o.MaxPathComponents = c.MaxPathComponents
		}
	}
}

type ImageIndexMirrorOption func(*IndexImageMirrorerOptions)

func DefaultImageIndexMirrorerOptions() *IndexImageMirrorerOptions {
	return &IndexImageMirrorerOptions{
		ManifestDir: filepath.Join(".", "manifests"),
	}
}

func WithMirrorer(i ImageMirrorer) ImageIndexMirrorOption {
	return func(o *IndexImageMirrorerOptions) {
		o.ImageMirrorer = i
	}
}

func WithExtractor(e DatabaseExtractor) ImageIndexMirrorOption {
	return func(o *IndexImageMirrorerOptions) {
		o.DatabaseExtractor = e
	}
}

func WithSource(s imagesource.TypedImageReference) ImageIndexMirrorOption {
	return func(o *IndexImageMirrorerOptions) {
		o.Source = s
	}
}

func WithDest(d imagesource.TypedImageReference) ImageIndexMirrorOption {
	return func(o *IndexImageMirrorerOptions) {
		o.Dest = d
	}
}

func WithManifestDir(d string) ImageIndexMirrorOption {
	return func(o *IndexImageMirrorerOptions) {
		o.ManifestDir = d
	}
}

func WithMaxPathComponents(i int) ImageIndexMirrorOption {
	return func(o *IndexImageMirrorerOptions) {
		o.MaxPathComponents = i
	}
}
