package internal

// This file manually vendors a lot of the parsing from library-go,
// with `Parse` changed to `ParseTarget`. See the description on `ParseTarget` for details.

import (
	"crypto"
	"errors"
	"fmt"
	"hash"
	"io"
	"regexp"
	"strings"

	imgref "github.com/openshift/library-go/pkg/image/reference"

	"github.com/openshift/oc/pkg/cli/image/imagesource"
)

// ParseTarget parses a Docker pull spec string into a DockerImageReference.
// It should be used when parsing a reference to be a target of a mirror operation.
// In the face of amiguity, it assumes the reference is a registry rather than a repo name.
// i.e quay.io will parse as {Registry: quay.io}, and not {Registry: docker.io, Namespace: library, Name: quay.io}
func ParseTarget(spec string) (imgref.DockerImageReference, error) {
	var ref imgref.DockerImageReference

	namedRef, err := ParseNamed(spec)
	if err != nil {
		return ref, err
	}

	name := namedRef.Name()
	i := strings.IndexRune(name, '/')

	// if there are no path components, and it looks like a url (contains a .) or localhost, it's a registry
	isRegistryOnly := i == -1 && (strings.ContainsAny(name, ".") || strings.HasPrefix(name, "localhost"))

	// if there are no path components, and it's not a registry, it's a name
	isNameOnly := i == -1 && !isRegistryOnly

	// if there are path components, and the first component doesn't look like a url, it's a name
	isNameOnly = isNameOnly || (i > -1 && (!strings.ContainsAny(name[:i], ":.") && name[:i] != "localhost"))

	if isRegistryOnly {
		ref.Registry = namedRef.String()
	} else if isNameOnly {
		ref.Name = name
	} else {
		ref.Registry, ref.Name = name[:i], name[i+1:]
	}

	if named, ok := namedRef.(NamedTagged); !isRegistryOnly && ok {
		ref.Tag = named.Tag()
	}

	if named, ok := namedRef.(Canonical); ok {
		ref.ID = named.Digest().String()
	}

	// It's not enough just to use the reference.ParseNamed(). We have to fill
	// ref.Namespace from ref.Name
	if i := strings.IndexRune(ref.Name, '/'); i != -1 {
		ref.Namespace, ref.Name = ref.Name[:i], ref.Name[i+1:]
	}

	return ref, nil
}

// ParseTargetReference is a variant of imagesource.ParseReference that uses `ParseTarget` to prefer
// parsing ambiguous names as registries
func ParseTargetReference(ref string) (imagesource.TypedImageReference, error) {
	dstType := imagesource.DestinationRegistry
	switch {
	case strings.HasPrefix(ref, "s3://"):
		dstType = imagesource.DestinationS3
		ref = strings.TrimPrefix(ref, "s3://")
	case strings.HasPrefix(ref, "file://"):
		dstType = imagesource.DestinationFile
		ref = strings.TrimPrefix(ref, "file://")
		if strings.HasPrefix(ref, "/") {
			ref = ref[1:]
		}
	}
	dst, err := ParseTarget(ref)
	if err != nil {
		return imagesource.TypedImageReference{Ref: dst, Type: dstType}, fmt.Errorf("%q is not a valid image reference: %v", ref, err)
	}
	return imagesource.TypedImageReference{Ref: dst, Type: dstType}, nil
}

// Vendored

const (
	// NameTotalLengthMax is the maximum total number of characters in a repository name.
	NameTotalLengthMax = 255
)

var (
	// ErrReferenceInvalidFormat represents an error while trying to parse a string as a reference.
	ErrReferenceInvalidFormat = errors.New("invalid reference format")

	// ErrTagInvalidFormat represents an error while trying to parse a string as a tag.
	ErrTagInvalidFormat = errors.New("invalid tag format")

	// ErrDigestInvalidFormat represents an error while trying to parse a string as a tag.
	ErrDigestInvalidFormat = errors.New("invalid digest format")

	// ErrNameContainsUppercase is returned for invalid repository names that contain uppercase characters.
	ErrNameContainsUppercase = errors.New("repository name must be lowercase")

	// ErrNameEmpty is returned for empty, invalid repository names.
	ErrNameEmpty = errors.New("repository name must have at least one component")

	// ErrNameTooLong is returned when a repository name is longer than NameTotalLengthMax.
	ErrNameTooLong = fmt.Errorf("repository name must not be more than %v characters", NameTotalLengthMax)
)

// Reference is an opaque object reference identifier that may include
// modifiers such as a hostname, name, tag, and digest.
type Reference interface {
	// String returns the full reference
	String() string
}

// ParseNamed parses s and returns a syntactically valid reference implementing
// the Named interface. The reference must have a name, otherwise an error is
// returned.
// If an error was encountered it is returned, along with a nil Reference.
// NOTE: ParseNamed will not handle short digests.
func ParseNamed(s string) (Named, error) {
	ref, err := Parse(s)
	if err != nil {
		return nil, err
	}
	named, isNamed := ref.(Named)
	if !isNamed {
		return nil, fmt.Errorf("reference %s has no name", ref.String())
	}
	return named, nil
}

// Parse parses s and returns a syntactically valid Reference.
// If an error was encountered it is returned, along with a nil Reference.
// NOTE: Parse will not handle short digests.
func Parse(s string) (Reference, error) {
	matches := ReferenceRegexp.FindStringSubmatch(s)
	if matches == nil {
		if s == "" {
			return nil, ErrNameEmpty
		}
		if ReferenceRegexp.FindStringSubmatch(strings.ToLower(s)) != nil {
			return nil, ErrNameContainsUppercase
		}
		return nil, ErrReferenceInvalidFormat
	}

	if len(matches[1]) > NameTotalLengthMax {
		return nil, ErrNameTooLong
	}

	ref := reference{
		name: matches[1],
		tag:  matches[2],
	}
	if matches[3] != "" {
		var err error
		ref.digest, err = ParseDigest(matches[3])
		if err != nil {
			return nil, err
		}
	}

	r := getBestReferenceType(ref)
	if r == nil {
		return nil, ErrNameEmpty
	}

	return r, nil
}

func getBestReferenceType(ref reference) Reference {
	if ref.name == "" {
		// Allow digest only references
		if ref.digest != "" {
			return digestReference(ref.digest)
		}
		return nil
	}
	if ref.tag == "" {
		if ref.digest != "" {
			return canonicalReference{
				name:   ref.name,
				digest: ref.digest,
			}
		}
		return repository(ref.name)
	}
	if ref.digest == "" {
		return taggedReference{
			name: ref.name,
			tag:  ref.tag,
		}
	}

	return ref
}

type reference struct {
	name   string
	tag    string
	digest Digest
}

func (r reference) String() string {
	return r.name + ":" + r.tag + "@" + r.digest.String()
}

func (r reference) Name() string {
	return r.name
}

func (r reference) Tag() string {
	return r.tag
}

func (r reference) Digest() Digest {
	return r.digest
}

type repository string

func (r repository) String() string {
	return string(r)
}

func (r repository) Name() string {
	return string(r)
}

type digestReference Digest

func (d digestReference) String() string {
	return string(d)
}

func (d digestReference) Digest() Digest {
	return Digest(d)
}

type taggedReference struct {
	name string
	tag  string
}

func (t taggedReference) String() string {
	return t.name + ":" + t.tag
}

func (t taggedReference) Name() string {
	return t.name
}

func (t taggedReference) Tag() string {
	return t.tag
}

type canonicalReference struct {
	name   string
	digest Digest
}

func (c canonicalReference) String() string {
	return c.name + "@" + c.digest.String()
}

func (c canonicalReference) Name() string {
	return c.name
}

func (c canonicalReference) Digest() Digest {
	return c.digest
}

// Named is an object with a full name
type Named interface {
	Reference
	Name() string
}

// Tagged is an object which has a tag
type Tagged interface {
	Reference
	Tag() string
}

// NamedTagged is an object including a name and tag.
type NamedTagged interface {
	Named
	Tag() string
}

// Digested is an object which has a digest
// in which it can be referenced by
type Digested interface {
	Reference
	Digest() Digest
}

// Canonical reference is an object with a fully unique
// name including a name with domain and digest
type Canonical interface {
	Named
	Digest() Digest
}

var (
	// alphaNumericRegexp defines the alpha numeric atom, typically a
	// component of names. This only allows lower case characters and digits.
	alphaNumericRegexp = match(`[a-z0-9]+`)

	// separatorRegexp defines the separators allowed to be embedded in name
	// components. This allow one period, one or two underscore and multiple
	// dashes.
	separatorRegexp = match(`(?:[._]|__|[-]*)`)

	// nameComponentRegexp restricts registry path component names to start
	// with at least one letter or number, with following parts able to be
	// separated by one period, one or two underscore and multiple dashes.
	nameComponentRegexp = expression(
		alphaNumericRegexp,
		optional(repeated(separatorRegexp, alphaNumericRegexp)))

	// hostnameComponentRegexp restricts the registry hostname component of a
	// repository name to start with a component as defined by hostnameRegexp
	// and followed by an optional port.
	hostnameComponentRegexp = match(`(?:[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])`)

	// hostnameRegexp defines the structure of potential hostname components
	// that may be part of image names. This is purposely a subset of what is
	// allowed by DNS to ensure backwards compatibility with Docker image
	// names.
	hostnameRegexp = expression(
		hostnameComponentRegexp,
		optional(repeated(literal(`.`), hostnameComponentRegexp)),
		optional(literal(`:`), match(`[0-9]+`)))

	// TagRegexp matches valid tag names. From docker/docker:graph/tags.go.
	TagRegexp = match(`[\w][\w.-]{0,127}`)

	// anchoredTagRegexp matches valid tag names, anchored at the start and
	// end of the matched string.
	anchoredTagRegexp = anchored(TagRegexp)

	// anchoredDigestRegexp matches valid digests, anchored at the start and
	// end of the matched string.
	anchoredDigestRegexp = anchored(DigestRegexp)

	// NameRegexp is the format for the name component of references. The
	// regexp has capturing groups for the hostname and name part omitting
	// the separating forward slash from either.
	NameRegexp = expression(
		optional(hostnameRegexp, literal(`/`)),
		nameComponentRegexp,
		optional(repeated(literal(`/`), nameComponentRegexp)))

	// anchoredNameRegexp is used to parse a name value, capturing the
	// hostname and trailing components.
	anchoredNameRegexp = anchored(
		optional(capture(hostnameRegexp), literal(`/`)),
		capture(nameComponentRegexp,
			optional(repeated(literal(`/`), nameComponentRegexp))))

	// ReferenceRegexp is the full supported format of a reference. The regexp
	// is anchored and has capturing groups for name, tag, and digest
	// components.
	ReferenceRegexp = anchored(capture(NameRegexp),
		optional(literal(":"), capture(TagRegexp)),
		optional(literal("@"), capture(DigestRegexp)))
)

// match compiles the string to a regular expression.
var match = regexp.MustCompile

// literal compiles s into a literal regular expression, escaping any regexp
// reserved characters.
func literal(s string) *regexp.Regexp {
	re := match(regexp.QuoteMeta(s))

	if _, complete := re.LiteralPrefix(); !complete {
		panic("must be a literal")
	}

	return re
}

// expression defines a full expression, where each regular expression must
// follow the previous.
func expression(res ...*regexp.Regexp) *regexp.Regexp {
	var s string
	for _, re := range res {
		s += re.String()
	}

	return match(s)
}

// optional wraps the expression in a non-capturing group and makes the
// production optional.
func optional(res ...*regexp.Regexp) *regexp.Regexp {
	return match(group(expression(res...)).String() + `?`)
}

// repeated wraps the regexp in a non-capturing group to get one or more
// matches.
func repeated(res ...*regexp.Regexp) *regexp.Regexp {
	return match(group(expression(res...)).String() + `+`)
}

// group wraps the regexp in a non-capturing group.
func group(res ...*regexp.Regexp) *regexp.Regexp {
	return match(`(?:` + expression(res...).String() + `)`)
}

// capture wraps the expression in a capturing group.
func capture(res ...*regexp.Regexp) *regexp.Regexp {
	return match(`(` + expression(res...).String() + `)`)
}

// anchored anchors the regular expression by adding start and end delimiters.
func anchored(res ...*regexp.Regexp) *regexp.Regexp {
	return match(`^` + expression(res...).String() + `$`)
}

const (
	// DigestSha256EmptyTar is the canonical sha256 digest of empty data
	DigestSha256EmptyTar = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// Digest allows simple protection of hex formatted digest strings, prefixed
// by their algorithm. Strings of type Digest have some guarantee of being in
// the correct format and it provides quick access to the components of a
// digest string.
//
// The following is an example of the contents of Digest types:
//
// 	sha256:7173b809ca12ec5dee4506cd86be934c4596dd234ee82c0662eac04a8c2c71dc
//
// This allows to abstract the digest behind this type and work only in those
// terms.
type Digest string

// NewDigest returns a Digest from alg and a hash.Hash object.
func NewDigest(alg Algorithm, h hash.Hash) Digest {
	return NewDigestFromBytes(alg, h.Sum(nil))
}

// NewDigestFromBytes returns a new digest from the byte contents of p.
// Typically, this can come from hash.Hash.Sum(...) or xxx.SumXXX(...)
// functions. This is also useful for rebuilding digests from binary
// serializations.
func NewDigestFromBytes(alg Algorithm, p []byte) Digest {
	return Digest(fmt.Sprintf("%s:%x", alg, p))
}

// NewDigestFromHex returns a Digest from alg and a the hex encoded digest.
func NewDigestFromHex(alg, hex string) Digest {
	return Digest(fmt.Sprintf("%s:%s", alg, hex))
}

// DigestRegexp matches valid digest types.
var DigestRegexp = regexp.MustCompile(`[a-zA-Z0-9-_+.]+:[a-fA-F0-9]+`)

// DigestRegexpAnchored matches valid digest types, anchored to the start and end of the match.
var DigestRegexpAnchored = regexp.MustCompile(`^` + DigestRegexp.String() + `$`)

var (

	// ErrDigestInvalidLength returned when digest has invalid length.
	ErrDigestInvalidLength = fmt.Errorf("invalid checksum digest length")

	// ErrDigestUnsupported returned when the digest algorithm is unsupported.
	ErrDigestUnsupported = fmt.Errorf("unsupported digest algorithm")
)

// ParseDigest parses s and returns the validated digest object. An error will
// be returned if the format is invalid.
func ParseDigest(s string) (Digest, error) {
	d := Digest(s)

	return d, d.Validate()
}

// FromReader returns the most valid digest for the underlying content using
// the canonical digest algorithm.
func FromReader(rd io.Reader) (Digest, error) {
	return CanonicalDigest.FromReader(rd)
}

// FromBytes digests the input and returns a Digest.
func FromBytes(p []byte) Digest {
	return CanonicalDigest.FromBytes(p)
}

// Validate checks that the contents of d is a valid digest, returning an
// error if not.
func (d Digest) Validate() error {
	s := string(d)

	if !DigestRegexpAnchored.MatchString(s) {
		return ErrDigestInvalidFormat
	}

	i := strings.Index(s, ":")
	if i < 0 {
		return ErrDigestInvalidFormat
	}

	// case: "sha256:" with no hex.
	if i+1 == len(s) {
		return ErrDigestInvalidFormat
	}

	switch algorithm := Algorithm(s[:i]); algorithm {
	case SHA256, SHA384, SHA512:
		if algorithm.Size()*2 != len(s[i+1:]) {
			return ErrDigestInvalidLength
		}
	default:
		return ErrDigestUnsupported
	}

	return nil
}

// Algorithm returns the algorithm portion of the digest. This will panic if
// the underlying digest is not in a valid format.
func (d Digest) Algorithm() Algorithm {
	return Algorithm(d[:d.sepIndex()])
}

// Hex returns the hex digest portion of the digest. This will panic if the
// underlying digest is not in a valid format.
func (d Digest) Hex() string {
	return string(d[d.sepIndex()+1:])
}

func (d Digest) String() string {
	return string(d)
}

func (d Digest) sepIndex() int {
	i := strings.Index(string(d), ":")

	if i < 0 {
		panic("could not find ':' in digest: " + d)
	}

	return i
}

// Algorithm identifies and implementation of a digester by an identifier.
// Note the that this defines both the hash algorithm used and the string
// encoding.
type Algorithm string

// supported digest types
const (
	SHA256 Algorithm = "sha256" // sha256 with hex encoding
	SHA384 Algorithm = "sha384" // sha384 with hex encoding
	SHA512 Algorithm = "sha512" // sha512 with hex encoding

	// CanonicalDigest is the primary digest algorithm used with the distribution
	// project. Other digests may be used but this one is the primary storage
	// digest.
	CanonicalDigest = SHA256
)

var (
	// TODO(stevvooe): Follow the pattern of the standard crypto package for
	// registration of digests. Effectively, we are a registerable set and
	// common symbol access.

	// algorithms maps values to hash.Hash implementations. Other algorithms
	// may be available but they cannot be calculated by the digest package.
	algorithms = map[Algorithm]crypto.Hash{
		SHA256: crypto.SHA256,
		SHA384: crypto.SHA384,
		SHA512: crypto.SHA512,
	}
)

// Available returns true if the digest type is available for use. If this
// returns false, New and Hash will return nil.
func (a Algorithm) Available() bool {
	h, ok := algorithms[a]
	if !ok {
		return false
	}

	// check availability of the hash, as well
	return h.Available()
}

func (a Algorithm) String() string {
	return string(a)
}

// Size returns number of bytes returned by the hash.
func (a Algorithm) Size() int {
	h, ok := algorithms[a]
	if !ok {
		return 0
	}
	return h.Size()
}

// Set implemented to allow use of Algorithm as a command line flag.
func (a *Algorithm) Set(value string) error {
	if value == "" {
		*a = CanonicalDigest
	} else {
		// just do a type conversion, support is queried with Available.
		*a = Algorithm(value)
	}

	return nil
}

// New returns a new digester for the specified algorithm. If the algorithm
// does not have a digester implementation, nil will be returned. This can be
// checked by calling Available before calling New.
func (a Algorithm) New() Digester {
	return &digester{
		alg:  a,
		hash: a.Hash(),
	}
}

// Hash returns a new hash as used by the algorithm. If not available, the
// method will panic. Check Algorithm.Available() before calling.
func (a Algorithm) Hash() hash.Hash {
	if !a.Available() {
		// NOTE(stevvooe): A missing hash is usually a programming error that
		// must be resolved at compile time. We don't import in the digest
		// package to allow users to choose their hash implementation (such as
		// when using stevvooe/resumable or a hardware accelerated package).
		//
		// Applications that may want to resolve the hash at runtime should
		// call Algorithm.Available before call Algorithm.Hash().
		panic(fmt.Sprintf("%v not available (make sure it is imported)", a))
	}

	return algorithms[a].New()
}

// FromReader returns the digest of the reader using the algorithm.
func (a Algorithm) FromReader(rd io.Reader) (Digest, error) {
	digester := a.New()

	if _, err := io.Copy(digester.Hash(), rd); err != nil {
		return "", err
	}

	return digester.Digest(), nil
}

// FromBytes digests the input and returns a Digest.
func (a Algorithm) FromBytes(p []byte) Digest {
	digester := a.New()

	if _, err := digester.Hash().Write(p); err != nil {
		// Writes to a Hash should never fail. None of the existing
		// hash implementations in the stdlib or hashes vendored
		// here can return errors from Write. Having a panic in this
		// condition instead of having FromBytes return an error value
		// avoids unnecessary error handling paths in all callers.
		panic("write to hash function returned error: " + err.Error())
	}

	return digester.Digest()
}

// TODO(stevvooe): Allow resolution of verifiers using the digest type and
// this registration system.

// Digester calculates the digest of written data. Writes should go directly
// to the return value of Hash, while calling Digest will return the current
// value of the digest.
type Digester interface {
	Hash() hash.Hash // provides direct access to underlying hash instance.
	Digest() Digest
}

// digester provides a simple digester definition that embeds a hasher.
type digester struct {
	alg  Algorithm
	hash hash.Hash
}

func (d *digester) Hash() hash.Hash {
	return d.hash
}

func (d *digester) Digest() Digest {
	return NewDigest(d.alg, d.hash)
}
