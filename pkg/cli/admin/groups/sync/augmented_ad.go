package sync

import (
	"github.com/go-ldap/ldap/v3"
	legacyconfigv1 "github.com/openshift/api/legacyconfig/v1"
	ldapquery "github.com/openshift/library-go/pkg/security/ldapquery"
	syncgroups "github.com/openshift/oc/pkg/helpers/groupsync"
	"github.com/openshift/oc/pkg/helpers/groupsync/ad"
	"github.com/openshift/oc/pkg/helpers/groupsync/interfaces"
)

var _ SyncBuilder = &AugmentedADBuilder{}
var _ PruneBuilder = &AugmentedADBuilder{}

type AugmentedADBuilder struct {
	LDAPClient ldap.Client
	Config     *legacyconfigv1.AugmentedActiveDirectoryConfig

	augmentedADLDAPInterface *ad.AugmentedADLDAPInterface
}

func (b *AugmentedADBuilder) GetGroupLister() (interfaces.LDAPGroupLister, error) {
	return b.getAugmentedADLDAPInterface()
}

func (b *AugmentedADBuilder) GetGroupNameMapper() (interfaces.LDAPGroupNameMapper, error) {
	ldapInterface, err := b.getAugmentedADLDAPInterface()
	if err != nil {
		return nil, err
	}
	if b.Config.GroupNameAttributes != nil {
		return syncgroups.NewEntryAttributeGroupNameMapper(b.Config.GroupNameAttributes, ldapInterface), nil
	}

	return nil, nil
}

func (b *AugmentedADBuilder) GetUserNameMapper() (interfaces.LDAPUserNameMapper, error) {
	return syncgroups.NewUserNameMapper(b.Config.UserNameAttributes), nil
}

func (b *AugmentedADBuilder) GetGroupMemberExtractor() (interfaces.LDAPMemberExtractor, error) {
	return b.getAugmentedADLDAPInterface()
}

func (b *AugmentedADBuilder) getAugmentedADLDAPInterface() (*ad.AugmentedADLDAPInterface, error) {
	if b.augmentedADLDAPInterface != nil {
		return b.augmentedADLDAPInterface, nil
	}

	userQuery, err := ldapquery.NewLDAPQuery(ToLDAPQuery(b.Config.AllUsersQuery))
	if err != nil {
		return nil, err
	}
	groupQuery, err := ldapquery.NewLDAPQueryOnAttribute(ToLDAPQuery(b.Config.AllGroupsQuery), b.Config.GroupUIDAttribute)
	if err != nil {
		return nil, err
	}
	b.augmentedADLDAPInterface = ad.NewAugmentedADLDAPInterface(b.LDAPClient,
		userQuery, b.Config.GroupMembershipAttributes, b.Config.UserNameAttributes,
		groupQuery, b.Config.GroupNameAttributes)
	return b.augmentedADLDAPInterface, nil
}

func (b *AugmentedADBuilder) GetGroupDetector() (interfaces.LDAPGroupDetector, error) {
	return b.getAugmentedADLDAPInterface()
}
