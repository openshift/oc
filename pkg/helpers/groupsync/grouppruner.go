package syncgroups

import (
	"context"
	"fmt"
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	userv1client "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"
	"github.com/openshift/oc/pkg/helpers/groupsync/interfaces"
)

// GroupPruner runs a prune job on Groups
type GroupPruner interface {
	Prune() (errors []error)
}

// LDAPGroupPruner prunes Groups referencing records on an external LDAP server
type LDAPGroupPruner struct {
	// Lists all groups to be synced
	GroupLister interfaces.LDAPGroupLister
	// Fetches a group and extracts object metainformation and membership list from a group
	GroupDetector interfaces.LDAPGroupDetector
	// Maps an LDAP group enrty to an OpenShift Group's Name
	GroupNameMapper interfaces.LDAPGroupNameMapper
	// Allows the Pruner to search for OpenShift Groups
	GroupClient userv1client.GroupInterface
	// Host stores the address:port of the LDAP server
	Host string
	// DryRun indicates that no changes should be made.
	DryRun bool

	// Out is used to provide output while the sync job is happening
	Out io.Writer
	Err io.Writer
}

var _ GroupPruner = &LDAPGroupPruner{}

// Prune allows the LDAPGroupPruner to be a GroupPruner
func (s *LDAPGroupPruner) Prune() []error {
	var errors []error

	// determine what to sync
	klog.V(1).Infof("LDAPGroupPruner listing groups to prune with %v", s.GroupLister)
	ldapGroupUIDs, err := s.GroupLister.ListGroups()
	if err != nil {
		errors = append(errors, err)
		return errors
	}
	klog.V(1).Infof("LDAPGroupPruner will attempt to prune ldapGroupUIDs %v", ldapGroupUIDs)

	for _, ldapGroupUID := range ldapGroupUIDs {
		klog.V(1).Infof("Checking LDAP group %v", ldapGroupUID)

		exists, err := s.GroupDetector.Exists(ldapGroupUID)
		if err != nil {
			fmt.Fprintf(s.Err, "Error determining LDAP group existence for group %q: %v.\n", ldapGroupUID, err)
			errors = append(errors, err)
			continue
		}
		if exists {
			continue
		}

		// if the LDAP entry that was previously used to create the group doesn't exist, prune it
		groupName, err := s.GroupNameMapper.GroupNameFor(ldapGroupUID)
		if err != nil {
			fmt.Fprintf(s.Err, "Error determining OpenShift group name for LDAP group %q: %v.\n", ldapGroupUID, err)
			errors = append(errors, err)
			continue
		}

		if !s.DryRun {
			if err := s.GroupClient.Delete(context.TODO(), groupName, metav1.DeleteOptions{}); err != nil {
				fmt.Fprintf(s.Err, "Error pruning OpenShift group %q: %v.\n", groupName, err)
				errors = append(errors, err)
				continue
			}
		}

		fmt.Fprintf(s.Out, "group/%s\n", groupName)
	}

	return errors
}
