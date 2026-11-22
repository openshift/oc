package set

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestUpdateDataForObject(t *testing.T) {
	t.Run("utf8 stays in data", func(t *testing.T) {
		// OCPBUGS-133414: non-ASCII UTF-8 must not move to binaryData.
		cm := &corev1.ConfigMap{
			Data: map[string]string{
				"greeting": "café",
				"key1":     "old",
			},
		}
		if _, err := updateDataForObject(cm, func(data map[string][]byte) error {
			data["key1"] = []byte("new")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := cm.Data["greeting"]; got != "café" {
			t.Fatalf("Data[greeting]=%q, want %q", got, "café")
		}
		if _, ok := cm.BinaryData["greeting"]; ok {
			t.Fatalf("greeting moved to BinaryData: %q", cm.BinaryData["greeting"])
		}
		if got := cm.Data["key1"]; got != "new" {
			t.Fatalf("Data[key1]=%q, want %q", got, "new")
		}
	})

	t.Run("invalid utf8 stays in binaryData", func(t *testing.T) {
		cm := &corev1.ConfigMap{}
		bin := []byte{0xff, 0xfe}
		if _, err := updateDataForObject(cm, func(data map[string][]byte) error {
			data["bin"] = bin
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cm.BinaryData["bin"], bin) {
			t.Fatalf("BinaryData[bin]=%v, want %v", cm.BinaryData["bin"], bin)
		}
		if _, ok := cm.Data["bin"]; ok {
			t.Fatalf("bin moved to Data: %q", cm.Data["bin"])
		}
	})

	t.Run("untouched binaryData is preserved", func(t *testing.T) {
		// Valid UTF-8 that used to be classified as binary; must stay put if unchanged.
		legacy := []byte{0x00, 'a'}
		cm := &corev1.ConfigMap{
			Data:       map[string]string{"key1": "old"},
			BinaryData: map[string][]byte{"legacy": append([]byte(nil), legacy...)},
		}
		if _, err := updateDataForObject(cm, func(data map[string][]byte) error {
			data["key1"] = []byte("new")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cm.BinaryData["legacy"], legacy) {
			t.Fatalf("BinaryData[legacy]=%v, want %v", cm.BinaryData["legacy"], legacy)
		}
		if _, ok := cm.Data["legacy"]; ok {
			t.Fatalf("legacy moved to Data: %q", cm.Data["legacy"])
		}
	})
}
