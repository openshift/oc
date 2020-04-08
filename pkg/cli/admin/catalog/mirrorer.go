package catalog

import (
	"fmt"
	"github.com/openshift/oc/pkg/cli/image/imagesource"
	"hash/fnv"
	"strings"

	"github.com/alicebob/sqlittle"
	"github.com/docker/distribution/reference"
	"k8s.io/apimachinery/pkg/util/errors"
)

type Mirrorer interface {
	Mirror() (map[string]Target, error)
}

// DatabaseExtractor knows how to pull an index image and extract its database
type DatabaseExtractor interface {
	Extract(from imagesource.TypedImageReference) (string, error)
}

type DatabaseExtractorFunc func(from imagesource.TypedImageReference) (string, error)

func (f DatabaseExtractorFunc) Extract(from imagesource.TypedImageReference) (string, error) {
	return f(from)
}

// Target determines the target to mirror to. We store both a tagged and digested target for different purposes.
// the digest is used for configuring cri-o to pull mirrored images, and the tag is required when mirroring
// to a registry so that the image does not get GC'd
type Target struct {
	WithDigest string
	WithTag    string
}

// ImageMirrorer knows how to mirror an image from one registry to another
type ImageMirrorer interface {
	Mirror(mapping map[string]Target) error
}

type ImageMirrorerFunc func(mapping map[string]Target) error

func (f ImageMirrorerFunc) Mirror(mapping map[string]Target) error {
	return f(mapping)
}

type IndexImageMirrorer struct {
	ImageMirrorer     ImageMirrorer
	DatabaseExtractor DatabaseExtractor

	// options
	Source, Dest      imagesource.TypedImageReference
	MaxPathComponents int
}

var _ Mirrorer = &IndexImageMirrorer{}

func NewIndexImageMirror(options ...ImageIndexMirrorOption) (*IndexImageMirrorer, error) {
	config := DefaultImageIndexMirrorerOptions()
	config.Apply(options)
	if err := config.Complete(); err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &IndexImageMirrorer{
		ImageMirrorer:     config.ImageMirrorer,
		DatabaseExtractor: config.DatabaseExtractor,
		Source:            config.Source,
		Dest:              config.Dest,
		MaxPathComponents: config.MaxPathComponents,
	}, nil
}

func (b *IndexImageMirrorer) Mirror() (map[string]Target, error) {
	dbFile, err := b.DatabaseExtractor.Extract(b.Source)
	if err != nil {
		return nil, err
	}

	images, err := imagesFromDb(dbFile)
	if err != nil {
		return nil, err
	}

	var errs = make([]error, 0)
	mapping, mapErrs := mappingForImages(images, b.Dest, b.MaxPathComponents)
	if len(mapErrs) > 0 {
		errs = append(errs, mapErrs...)
	}

	if err := b.ImageMirrorer.Mirror(mapping); err != nil {
		errs = append(errs, fmt.Errorf("mirroring failed: %s", err.Error()))
	}

	return mapping, errors.NewAggregate(errs)
}

func imagesFromDb(file string) (map[string]struct{}, error) {
	db, err := sqlittle.Open(file)
	if err != nil {
		return nil, err
	}

	// get all images
	var images = make(map[string]struct{}, 0)
	var errs = make([]error, 0)
	reader := func(r sqlittle.Row) {
		var image string
		if err := r.Scan(&image); err != nil {
			errs = append(errs, err)
			return
		}
		if image != "" {
			images[image] = struct{}{}
		}
	}
	if err := db.Select("related_image", reader, "image"); err != nil {
		errs = append(errs, err)
		return nil, errors.NewAggregate(errs)
	}

	// get all bundlepaths
	if err := db.Select("operatorbundle", reader, "bundlepath"); err != nil {
		errs = append(errs, err)
		return nil, errors.NewAggregate(errs)
	}
	return images, nil
}

func mappingForImages(images map[string]struct{}, dest imagesource.TypedImageReference, maxComponents int) (mapping map[string]Target, errs []error) {
	domain := dest.Ref.Registry

	// handle bare repository targets
	if !strings.Contains(dest.String(), "/") {
		domain = dest.String()
	}

	destComponents := make([]string, 0)
	for _, s := range strings.Split(strings.TrimPrefix(dest.String(), domain), "/") {
		if s != "" {
			destComponents = append(destComponents, s)
		}
	}

	mapping = map[string]Target{}
	hasher := fnv.New32a()
	for img := range images {
		if img == "" {
			continue
		}
		ref, err := reference.ParseNormalizedNamed(img)
		if err != nil {
			errs = append(errs, fmt.Errorf("couldn't parse image for mirroring (%s), skipping mirror: %v", img, err))
			continue
		}

		components := append(destComponents, strings.Split(reference.Path(ref), "/")...)
		if len(components) < 2 {
			errs = append(errs, fmt.Errorf("couldn't parse image path components for mirroring (%s), skipping mirror", img))
			continue
		}

		// calculate a new path in the target registry, where only the first (max path components - 1) components are
		// allowed, and the rest of the path components are collapsed into a single longer component
		parts := []string{domain}
		if maxComponents < 1 {
			parts = append(parts, components...)
		} else {
			parts = append(parts, components[0:maxComponents-1]...)
			parts = append(parts, strings.Join(components[maxComponents-1:], "-"))
		}
		name := strings.TrimSuffix(strings.Join(parts, "/"), "/")

		var target Target
		// if ref has a tag, generate a target with the same tag
		if c, ok := ref.(reference.NamedTagged); ok {
			target = Target{
				WithTag: name + ":" + c.Tag(),
			}
		} else {
			// Tag with the hash of the source ref
			hasher.Reset()
			_, err = hasher.Write([]byte(ref.String()))
			if err != nil {
				errs = append(errs, fmt.Errorf("couldn't generate tag for image (%s), skipping mirror", img))
				continue
			}
			target = Target{
				WithTag: name + ":" + fmt.Sprintf("%x", hasher.Sum32()),
			}
		}

		// if ref has a digest, generate a target with digest as well
		if c, ok := ref.(reference.Canonical); ok {
			target.WithDigest = name + "@" + c.Digest().String()
		}

		mapping[ref.String()] = target
	}
	return
}
