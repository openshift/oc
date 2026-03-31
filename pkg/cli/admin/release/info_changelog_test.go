package release

import (
	"bytes"
	"encoding/json"
	"testing"

	digest "github.com/opencontainers/go-digest"
	imageapi "github.com/openshift/api/image/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func featureGateManifest(clusterProfile, featureSet string, enabled, disabled []string) []byte {
	enabledItems := ""
	for _, fg := range enabled {
		enabledItems += `      - name: ` + fg + "\n"
	}
	disabledItems := ""
	for _, fg := range disabled {
		disabledItems += `      - name: ` + fg + "\n"
	}
	featureSetField := ""
	if featureSet != "" && featureSet != "Default" {
		featureSetField = "  featureSet: " + featureSet + "\n"
	}
	annotations := ""
	if clusterProfile != "" {
		annotations = `  annotations:
    include.release.openshift.io/` + clusterProfile + `: "true"
`
	}
	return []byte(`apiVersion: config.openshift.io/v1
kind: FeatureGate
metadata:
  name: cluster
` + annotations + `spec:
` + featureSetField + `status:
  featureGates:
    - version: "4.22"
      enabled:
` + enabledItems + `      disabled:
` + disabledItems)
}

func TestDescribeChangelogFeatureGatesJSON(t *testing.T) {
	fromManifest := featureGateManifest(
		"self-managed-high-availability",
		"",
		[]string{"ExistingEnabled"},
		[]string{"ExistingDisabled", "WillBeEnabled"},
	)
	toManifest := featureGateManifest(
		"self-managed-high-availability",
		"",
		[]string{"ExistingEnabled", "WillBeEnabled"},
		[]string{"ExistingDisabled"},
	)

	fromDigest := digest.FromString("from")
	toDigest := digest.FromString("to")

	diff := &ReleaseDiff{
		From: &ReleaseInfo{
			Digest: fromDigest,
			References: &imageapi.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
				},
				Spec: imageapi.ImageStreamSpec{
					Tags: []imageapi.TagReference{{Name: "from-tag"}},
				},
			},
			ManifestFiles: map[string][]byte{
				"0000_50_cluster-config-api_featureGate-Default-SelfManagedHA.yaml": fromManifest,
			},
			ComponentVersions: ComponentVersions{},
		},
		To: &ReleaseInfo{
			Digest: toDigest,
			References: &imageapi.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
				},
				Spec: imageapi.ImageStreamSpec{
					Tags: []imageapi.TagReference{{Name: "to-tag"}},
				},
			},
			ManifestFiles: map[string][]byte{
				"0000_50_cluster-config-api_featureGate-Default-SelfManagedHA.yaml": toManifest,
			},
			ComponentVersions: ComponentVersions{},
		},
		ChangedImages:    map[string]*ImageReferenceDiff{},
		ChangedManifests: map[string]*ReleaseManifestDiff{},
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	err := describeChangelog(out, errOut, &ReleaseInfo{}, diff, "", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, errOut.String())
	}

	var changeLog ChangeLog
	if err := json.Unmarshal(out.Bytes(), &changeLog); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v\noutput: %s", err, out.String())
	}

	if len(changeLog.FeatureGates) == 0 {
		t.Fatal("expected featureGates in JSON output, got none")
	}

	// WillBeEnabled changed from Disabled to Enabled
	found := false
	for _, fg := range changeLog.FeatureGates {
		if fg.Name == "WillBeEnabled" {
			found = true
			status, ok := fg.Status["SelfManagedHA"]
			if !ok {
				t.Fatalf("expected SelfManagedHA cluster profile, got: %v", fg.Status)
			}
			defaultStatus, ok := status["Default"]
			if !ok {
				t.Fatalf("expected Default feature set, got: %v", status)
			}
			if defaultStatus != "Enabled (Changed)" {
				t.Errorf("expected 'Enabled (Changed)' for WillBeEnabled, got %q", defaultStatus)
			}
		}
	}
	if !found {
		t.Errorf("expected WillBeEnabled in feature gates, got: %v", changeLog.FeatureGates)
	}
}

func TestDescribeChangelogNoFeatureGatesJSON(t *testing.T) {
	fromDigest := digest.FromString("from")
	toDigest := digest.FromString("to")

	diff := &ReleaseDiff{
		From: &ReleaseInfo{
			Digest: fromDigest,
			References: &imageapi.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
				},
				Spec: imageapi.ImageStreamSpec{
					Tags: []imageapi.TagReference{{Name: "from-tag"}},
				},
			},
			ManifestFiles:     map[string][]byte{},
			ComponentVersions: ComponentVersions{},
		},
		To: &ReleaseInfo{
			Digest: toDigest,
			References: &imageapi.ImageStream{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
				},
				Spec: imageapi.ImageStreamSpec{
					Tags: []imageapi.TagReference{{Name: "to-tag"}},
				},
			},
			ManifestFiles:     map[string][]byte{},
			ComponentVersions: ComponentVersions{},
		},
		ChangedImages:    map[string]*ImageReferenceDiff{},
		ChangedManifests: map[string]*ReleaseManifestDiff{},
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	err := describeChangelog(out, errOut, &ReleaseInfo{}, diff, "", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var changeLog ChangeLog
	if err := json.Unmarshal(out.Bytes(), &changeLog); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v", err)
	}

	if len(changeLog.FeatureGates) != 0 {
		t.Errorf("expected no featureGates when no manifests present, got %d", len(changeLog.FeatureGates))
	}
}
