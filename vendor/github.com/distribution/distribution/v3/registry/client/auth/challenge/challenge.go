// Package challenge re-exports the WWW-Authenticate challenge parsing that
// upstream moved to internal/client/auth/challenge in distribution v3. See
// package registry/client for the rationale and the removal condition.
package challenge

import (
	"net/http"

	"github.com/distribution/distribution/v3/internal/client/auth/challenge"
)

// Challenge carries information from a WWW-Authenticate response header.
type Challenge = challenge.Challenge

// Manager manages the challenges for endpoints.
type Manager = challenge.Manager

// FilteringManager wraps a Manager and drops challenges that do not pass the
// keep predicate.
type FilteringManager = challenge.FilteringManager

// NewSimpleManager returns an instance of Manager which only maps endpoints
// to challenges based on the responses which have been added the manager.
func NewSimpleManager() Manager {
	return challenge.NewSimpleManager()
}

// NewFilteringManager wraps base so that only challenges accepted by keep are
// returned.
func NewFilteringManager(base Manager, keep func(Challenge) bool) Manager {
	return challenge.NewFilteringManager(base, keep)
}

// ResponseChallenges returns a list of authorization challenges for the
// given http Response.
func ResponseChallenges(resp *http.Response) []Challenge {
	return challenge.ResponseChallenges(resp)
}
