package recommend

import (
	"context"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type check func(ctx context.Context) ([]metav1.Condition, error)

// precheck runs a set of Condition-generating checkers, and
// aggregates their results.  The Conditions must use True for happy
// signals, False for sad signals, and Unknown when we do not have enough
// information to make a happy-or-sad determination.
func (o *options) precheck(ctx context.Context) ([]metav1.Condition, error) {
	var conditions []metav1.Condition
	var errs []error
	for _, check := range []check{
		o.alerts,
	} {
		cs, err := check(ctx)
		if err != nil {
			errs = append(errs, err)
		}
		conditions = append(conditions, cs...)
	}

	return conditions, errors.Join(errs...)
}
