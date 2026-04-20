package ldap

import (
	"bytes"
	"testing"

	legacyconfigv1 "github.com/openshift/api/legacyconfig/v1"
	"github.com/openshift/library-go/pkg/config/helpers"
)

func TestValidateRFC2307Config_GroupUIDAttributeDN_NoFilter_FromYAML(t *testing.T) {
	configYAML := `
kind: LDAPSyncConfig
apiVersion: v1
url: ldap://LDAP_SERVICE_IP:389
insecure: true
bindDN: cn=admin,dc=example,dc=com
bindPassword: test
rfc2307:
    groupsQuery:
        baseDN: "ou=groups,dc=example,dc=com"
        scope: sub
        derefAliases: never
        pageSize: 0
    groupUIDAttribute: dn
    groupNameAttributes: [ cn ]
    groupMembershipAttributes: [ member ]
    usersQuery:
        baseDN: "ou=users,dc=example,dc=com"
        scope: sub
        derefAliases: never
        pageSize: 0
    userUIDAttribute: dn
    userNameAttributes: [ mail ]
    tolerateMemberNotFoundErrors: false
    tolerateMemberOutOfScopeErrors: false
`

	obj, err := helpers.ReadYAML(bytes.NewBufferString(configYAML), legacyconfigv1.InstallLegacy)
	if err != nil {
		t.Fatalf("failed to parse LDAPSyncConfig YAML: %v", err)
	}

	syncConfig, ok := obj.(*legacyconfigv1.LDAPSyncConfig)
	if !ok {
		t.Fatalf("expected *legacyconfigv1.LDAPSyncConfig, got %T", obj)
	}

	if syncConfig.RFC2307Config == nil {
		t.Fatal("expected RFC2307Config to be set")
	}

	results := ValidateRFC2307Config(syncConfig.RFC2307Config)
	if len(results.Errors) > 0 {
		t.Errorf("expected no validation errors when groupUIDAttribute is 'dn' and no filter is set, got: %v", results.Errors)
	}
}
