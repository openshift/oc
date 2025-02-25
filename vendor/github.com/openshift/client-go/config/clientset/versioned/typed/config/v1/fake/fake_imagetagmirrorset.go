// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1 "github.com/openshift/api/config/v1"
	configv1 "github.com/openshift/client-go/config/applyconfigurations/config/v1"
	typedconfigv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	gentype "k8s.io/client-go/gentype"
)

// fakeImageTagMirrorSets implements ImageTagMirrorSetInterface
type fakeImageTagMirrorSets struct {
	*gentype.FakeClientWithListAndApply[*v1.ImageTagMirrorSet, *v1.ImageTagMirrorSetList, *configv1.ImageTagMirrorSetApplyConfiguration]
	Fake *FakeConfigV1
}

func newFakeImageTagMirrorSets(fake *FakeConfigV1) typedconfigv1.ImageTagMirrorSetInterface {
	return &fakeImageTagMirrorSets{
		gentype.NewFakeClientWithListAndApply[*v1.ImageTagMirrorSet, *v1.ImageTagMirrorSetList, *configv1.ImageTagMirrorSetApplyConfiguration](
			fake.Fake,
			"",
			v1.SchemeGroupVersion.WithResource("imagetagmirrorsets"),
			v1.SchemeGroupVersion.WithKind("ImageTagMirrorSet"),
			func() *v1.ImageTagMirrorSet { return &v1.ImageTagMirrorSet{} },
			func() *v1.ImageTagMirrorSetList { return &v1.ImageTagMirrorSetList{} },
			func(dst, src *v1.ImageTagMirrorSetList) { dst.ListMeta = src.ListMeta },
			func(list *v1.ImageTagMirrorSetList) []*v1.ImageTagMirrorSet {
				return gentype.ToPointerSlice(list.Items)
			},
			func(list *v1.ImageTagMirrorSetList, items []*v1.ImageTagMirrorSet) {
				list.Items = gentype.FromPointerSlice(items)
			},
		),
		fake,
	}
}
