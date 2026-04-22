package ldap

import (
	"testing"

	legacyconfigv1 "github.com/openshift/api/legacyconfig/v1"
)

func TestValidateRFC2307Config_GroupUIDAttributeDN_NoFilter(t *testing.T) {
	config := &legacyconfigv1.RFC2307Config{
		AllGroupsQuery: legacyconfigv1.LDAPQuery{
			BaseDN:       "ou=groups,dc=example,dc=com",
			Scope:        "sub",
			DerefAliases: "never",
			PageSize:     0,
		},
		GroupUIDAttribute:         "dn",
		GroupNameAttributes:       []string{"cn"},
		GroupMembershipAttributes: []string{"member"},
		AllUsersQuery: legacyconfigv1.LDAPQuery{
			BaseDN:       "ou=users,dc=example,dc=com",
			Scope:        "sub",
			DerefAliases: "never",
			PageSize:     0,
		},
		UserUIDAttribute:               "dn",
		UserNameAttributes:             []string{"mail"},
		TolerateMemberNotFoundErrors:   false,
		TolerateMemberOutOfScopeErrors: false,
	}

	results := ValidateRFC2307Config(config)
	if len(results.Errors) > 0 {
		t.Errorf("expected no validation errors when groupUIDAttribute is 'dn' and no filter is set, got: %v", results.Errors)
	}
}

func TestValidateRFC2307Config_GroupUIDAttributeDN_WithValidFilter(t *testing.T) {
	config := &legacyconfigv1.RFC2307Config{
		AllGroupsQuery: legacyconfigv1.LDAPQuery{
			BaseDN:       "ou=groups,dc=example,dc=com",
			Scope:        "sub",
			DerefAliases: "never",
			PageSize:     0,
			Filter:       "(objectclass=groupOfNames)",
		},
		GroupUIDAttribute:         "dn",
		GroupNameAttributes:       []string{"cn"},
		GroupMembershipAttributes: []string{"member"},
		AllUsersQuery: legacyconfigv1.LDAPQuery{
			BaseDN:       "ou=users,dc=example,dc=com",
			Scope:        "sub",
			DerefAliases: "never",
			PageSize:     0,
		},
		UserUIDAttribute:               "dn",
		UserNameAttributes:             []string{"mail"},
		TolerateMemberNotFoundErrors:   false,
		TolerateMemberOutOfScopeErrors: false,
	}

	results := ValidateRFC2307Config(config)
	if len(results.Errors) > 0 {
		t.Errorf("expected no validation errors when groupUIDAttribute is 'dn' and a valid filter is set, got: %v", results.Errors)
	}
}
