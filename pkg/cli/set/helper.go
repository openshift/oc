package set

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/kubectl/pkg/scheme"
)

func selectContainers(containers []corev1.Container, spec string) ([]*corev1.Container, []*corev1.Container) {
	out := []*corev1.Container{}
	skipped := []*corev1.Container{}
	for i, c := range containers {
		if selectString(c.Name, spec) {
			out = append(out, &containers[i])
		} else {
			skipped = append(skipped, &containers[i])
		}
	}
	return out, skipped
}

// selectString returns true if the provided string matches spec, where spec is a string with
// a non-greedy '*' wildcard operator.
// TODO: turn into a regex and handle greedy matches and backtracking.
func selectString(s, spec string) bool {
	if spec == "*" {
		return true
	}
	if !strings.Contains(spec, "*") {
		return s == spec
	}

	pos := 0
	match := true
	parts := strings.Split(spec, "*")
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		next := strings.Index(s[pos:], part)
		switch {
		// next part not in string
		case next < pos:
			fallthrough
		// first part does not match start of string
		case i == 0 && pos != 0:
			fallthrough
		// last part does not exactly match remaining part of string
		case i == (len(parts)-1) && len(s) != (len(part)+next):
			match = false
			break
		default:
			pos = next
		}
	}
	return match
}

func updateEnv(existing []corev1.EnvVar, env []corev1.EnvVar, remove []string) []corev1.EnvVar {
	out := []corev1.EnvVar{}
	covered := sets.NewString(remove...)
	for _, e := range existing {
		if covered.Has(e.Name) {
			continue
		}
		newer, ok := findEnv(env, e.Name)
		if ok {
			covered.Insert(e.Name)
			out = append(out, newer)
			continue
		}
		out = append(out, e)
	}
	for _, e := range env {
		if covered.Has(e.Name) {
			continue
		}
		covered.Insert(e.Name)
		out = append(out, e)
	}
	return out
}

func findEnv(env []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, e := range env {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

// Patch represents the result of a mutation to an object.
type Patch struct {
	Info *resource.Info
	Err  error

	Before []byte
	After  []byte
	Patch  []byte
}

// CalculatePatches calls the mutation function on each provided info object, and generates a strategic merge patch for
// the changes in the object. Encoder must be able to encode the info into the appropriate destination type. If mutateFn
// returns false, the object is not included in the final list of patches.
func CalculatePatches(infos []*resource.Info, encoder runtime.Encoder, mutateFn func(*resource.Info) (bool, error)) []*Patch {
	var patches []*Patch
	for _, info := range infos {
		patch := &Patch{Info: info}

		versionedEncoder := scheme.Codecs.EncoderForVersion(encoder, patch.Info.Mapping.GroupVersionKind.GroupVersion())
		patch.Before, patch.Err = runtime.Encode(versionedEncoder, info.Object)

		ok, err := mutateFn(info)
		if !ok {
			continue
		}
		if err != nil {
			patch.Err = err
		}
		patches = append(patches, patch)
		if patch.Err != nil {
			continue
		}

		patch.After, patch.Err = runtime.Encode(versionedEncoder, info.Object)
		if patch.Err != nil {
			continue
		}

		// TODO: should be via New
		versioned, err := scheme.Scheme.ConvertToVersion(info.Object, info.Mapping.GroupVersionKind.GroupVersion())
		if err != nil {
			patch.Err = err
			continue
		}

		patch.Patch, patch.Err = strategicpatch.CreateTwoWayMergePatch(patch.Before, patch.After, versioned)
	}
	return patches
}

// CalculatePatchesExternal calls the mutation function on each provided info object, and generates a strategic merge patch for
// the changes in the object. Encoder must be able to encode the info into the appropriate destination type. If mutateFn
// returns false, the object is not included in the final list of patches.
func CalculatePatchesExternal(infos []*resource.Info, mutateFn func(*resource.Info) (bool, error)) []*Patch {
	var patches []*Patch
	for _, info := range infos {
		patch := &Patch{Info: info}

		patch.Before, patch.Err = runtime.Encode(scheme.DefaultJSONEncoder(), info.Object)

		ok, err := mutateFn(info)
		if !ok {
			continue
		}
		if err != nil {
			patch.Err = err
		}
		patches = append(patches, patch)
		if patch.Err != nil {
			continue
		}

		patch.After, patch.Err = runtime.Encode(scheme.DefaultJSONEncoder(), info.Object)
		if patch.Err != nil {
			continue
		}

		patch.Patch, patch.Err = strategicpatch.CreateTwoWayMergePatch(patch.Before, patch.After, info.Object)
	}
	return patches
}

func getObjectName(info *resource.Info) string {
	if info.Mapping != nil {
		return fmt.Sprintf("%s/%s", info.Mapping.Resource.Resource, info.Name)
	}
	gvk := info.Object.GetObjectKind().GroupVersionKind()
	if len(gvk.Group) == 0 {
		return fmt.Sprintf("%s/%s", strings.ToLower(gvk.Kind), info.Name)
	}
	return fmt.Sprintf("%s.%s/%s\n", strings.ToLower(gvk.Kind), gvk.Group, info.Name)
}
