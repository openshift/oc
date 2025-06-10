package auth

import (
	"fmt"
	"strings"
)

// Scope is a type which is serializable to a string
// using the allow scope grammar.
type Scope interface {
	String() string
}

// RepositoryScope represents a token scope for access
// to a repository.
type RepositoryScope struct {
	Repository string
	Class      string
	Actions    []string
}

// String returns the string representation of the repository
// using the scope grammar
func (rs RepositoryScope) String() string {
	repoType := "repository"
	// Keep existing format for image class to maintain backwards compatibility
	// with authorization servers which do not support the expanded grammar.
	if rs.Class != "" && rs.Class != "image" {
		repoType = fmt.Sprintf("%s(%s)", repoType, rs.Class)
	}
	return fmt.Sprintf("%s:%s:%s", repoType, rs.Repository, strings.Join(rs.Actions, ","))
}
