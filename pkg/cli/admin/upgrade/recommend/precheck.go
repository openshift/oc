package recommend

import (
	"context"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// condition extends metav1.Condition and a risk-acceptance name.
type condition struct {
	metav1.Condition

	// riskAcceptanceName should be set for Status != True
	// conditions, to give users a way to identify unhappy conditions
	// they are willing to accept.  The value does not need to be
	// unique among conditions, for example, multiple alerts with
	// the same name could all use the same risk acceptance name.
	riskAcceptanceName string
}

type check func(ctx context.Context) ([]condition, error)

// precheck runs a set of condition-generating checkers, and
// aggregates their results.  The conditions must use True for happy
// signals, False for sad signals, and Unknown when we do not have enough
// information to make a happy-or-sad determination.
func (o *options) precheck(ctx context.Context) ([]condition, error) {
	var conditions []condition
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
