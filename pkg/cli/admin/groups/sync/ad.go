package sync

import (
	"github.com/go-ldap/ldap/v3"
	legacyconfigv1 "github.com/openshift/api/legacyconfig/v1"
	ldapquery "github.com/openshift/library-go/pkg/security/ldapquery"
	syncgroups "github.com/openshift/oc/pkg/helpers/groupsync"
	"github.com/openshift/oc/pkg/helpers/groupsync/ad"
	"github.com/openshift/oc/pkg/helpers/groupsync/interfaces"
)

var _ SyncBuilder = &ADBuilder{}
var _ PruneBuilder = &ADBuilder{}

type ADBuilder struct {
	LDAPClient ldap.Client
	Config     *legacyconfigv1.ActiveDirectoryConfig

	adLDAPInterface *ad.ADLDAPInterface
}

func (b *ADBuilder) GetGroupLister() (interfaces.LDAPGroupLister, error) {
	return b.getADLDAPInterface()
}

func (b *ADBuilder) GetGroupNameMapper() (interfaces.LDAPGroupNameMapper, error) {
	return &syncgroups.DNLDAPGroupNameMapper{}, nil
}

func (b *ADBuilder) GetUserNameMapper() (interfaces.LDAPUserNameMapper, error) {
	return syncgroups.NewUserNameMapper(b.Config.UserNameAttributes), nil
}

func (b *ADBuilder) GetGroupMemberExtractor() (interfaces.LDAPMemberExtractor, error) {
	return b.getADLDAPInterface()
}

func (b *ADBuilder) getADLDAPInterface() (*ad.ADLDAPInterface, error) {
	if b.adLDAPInterface != nil {
		return b.adLDAPInterface, nil
	}

	userQuery, err := ldapquery.NewLDAPQuery(ToLDAPQuery(b.Config.AllUsersQuery))
	if err != nil {
		return nil, err
	}
	b.adLDAPInterface = ad.NewADLDAPInterface(b.LDAPClient,
		userQuery, b.Config.GroupMembershipAttributes, b.Config.UserNameAttributes)
	return b.adLDAPInterface, nil

}

func (b *ADBuilder) GetGroupDetector() (interfaces.LDAPGroupDetector, error) {
	return b.getADLDAPInterface()
}
