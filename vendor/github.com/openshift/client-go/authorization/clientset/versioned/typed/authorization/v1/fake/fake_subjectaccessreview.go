// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	context "context"

	v1 "github.com/openshift/api/authorization/v1"
	authorizationv1 "github.com/openshift/client-go/authorization/clientset/versioned/typed/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gentype "k8s.io/client-go/gentype"
	testing "k8s.io/client-go/testing"
)

// fakeSubjectAccessReviews implements SubjectAccessReviewInterface
type fakeSubjectAccessReviews struct {
	*gentype.FakeClient[*v1.SubjectAccessReview]
	Fake *FakeAuthorizationV1
}

func newFakeSubjectAccessReviews(fake *FakeAuthorizationV1) authorizationv1.SubjectAccessReviewInterface {
	return &fakeSubjectAccessReviews{
		gentype.NewFakeClient[*v1.SubjectAccessReview](
			fake.Fake,
			"",
			v1.SchemeGroupVersion.WithResource("subjectaccessreviews"),
			v1.SchemeGroupVersion.WithKind("SubjectAccessReview"),
			func() *v1.SubjectAccessReview { return &v1.SubjectAccessReview{} },
		),
		fake,
	}
}

// Create takes the representation of a subjectAccessReview and creates it.  Returns the server's representation of the subjectAccessReviewResponse, and an error, if there is any.
func (c *fakeSubjectAccessReviews) Create(ctx context.Context, subjectAccessReview *v1.SubjectAccessReview, opts metav1.CreateOptions) (result *v1.SubjectAccessReviewResponse, err error) {
	emptyResult := &v1.SubjectAccessReviewResponse{}
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateActionWithOptions(c.Resource(), subjectAccessReview, opts), emptyResult)
	if obj == nil {
		return emptyResult, err
	}
	return obj.(*v1.SubjectAccessReviewResponse), err
}
