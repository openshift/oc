package syncgroups

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"gopkg.in/ldap.v2"
	"k8s.io/klog"

	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	userv1 "github.com/openshift/api/user/v1"
	userv1client "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"
	"github.com/openshift/oc/pkg/helpers/groupsync/interfaces"
)

// GroupSyncer runs a Sync job on Groups
type GroupSyncer interface {
	// Sync syncs groups in OpenShift with records from an external source
	Sync() (groupsAffected []*userv1.Group, errors []error)
}

// LDAPGroupSyncer sync Groups with records on an external LDAP server
type LDAPGroupSyncer struct {
	// Lists all groups to be synced
	GroupLister interfaces.LDAPGroupLister
	// Fetches a group and extracts object metainformation and membership list from a group
	GroupMemberExtractor interfaces.LDAPMemberExtractor
	// Maps an LDAP user entry to an OpenShift User's Name
	UserNameMapper interfaces.LDAPUserNameMapper
	// Maps an LDAP group enrty to an OpenShift Group's Name
	GroupNameMapper interfaces.LDAPGroupNameMapper
	// Allows the Syncer to search for OpenShift Groups
	GroupClient userv1client.GroupInterface
	// Host stores the address:port of the LDAP server
	Host string
	// DryRun indicates that no changes should be made.
	DryRun bool

	// Out is used to provide output while the sync job is happening
	Out io.Writer
	Err io.Writer
}

var _ GroupSyncer = &LDAPGroupSyncer{}

// Sync allows the LDAPGroupSyncer to be a GroupSyncer
func (s *LDAPGroupSyncer) Sync() ([]*userv1.Group, []error) {
	openshiftGroups := []*userv1.Group{}
	var errors []error

	// determine what to sync
	klog.V(1).Infof("Listing with %v", s.GroupLister)
	ldapGroupUIDs, err := s.GroupLister.ListGroups()
	if err != nil {
		errors = append(errors, err)
		return nil, errors
	}
	klog.V(1).Infof("Sync ldapGroupUIDs %v", ldapGroupUIDs)

	for _, ldapGroupUID := range ldapGroupUIDs {
		klog.V(1).Infof("Checking LDAP group %v", ldapGroupUID)

		// get membership data
		memberEntries, err := s.GroupMemberExtractor.ExtractMembers(ldapGroupUID)
		if err != nil {
			fmt.Fprintf(s.Err, "Error determining LDAP group membership for %q: %v.\n", ldapGroupUID, err)
			errors = append(errors, err)
			continue
		}

		// determine OpenShift Users' usernames for LDAP group members
		usernames, err := s.determineUsernames(memberEntries)
		if err != nil {
			fmt.Fprintf(s.Err, "Error determining usernames for LDAP group %q: %v.\n", ldapGroupUID, err)
			errors = append(errors, err)
			continue
		}
		klog.V(1).Infof("Has OpenShift users %v", usernames)

		// update the OpenShift Group corresponding to this record
		openshiftGroup, err := s.makeOpenShiftGroup(ldapGroupUID, usernames)
		if err != nil {
			fmt.Fprintf(s.Err, "Error building OpenShift group for LDAP group %q: %v.\n", ldapGroupUID, err)
			errors = append(errors, err)
			continue
		}
		openshiftGroups = append(openshiftGroups, openshiftGroup)

		if !s.DryRun {
			fmt.Fprintf(s.Out, "group/%s\n", openshiftGroup.Name)
			if err := s.updateOpenShiftGroup(openshiftGroup); err != nil {
				fmt.Fprintf(s.Err, "Error updating OpenShift group %q for LDAP group %q: %v.\n", openshiftGroup.Name, ldapGroupUID, err)
				errors = append(errors, err)
				continue
			}
		}
	}

	return openshiftGroups, errors
}

// determineUsers determines the OpenShift Users that correspond to a list of LDAP member entries
func (s *LDAPGroupSyncer) determineUsernames(members []*ldap.Entry) ([]string, error) {
	var usernames []string
	for _, member := range members {
		username, err := s.UserNameMapper.UserNameFor(member)
		if err != nil {
			return nil, err
		}
		klog.V(2).Infof("Found OpenShift username %q for LDAP user for %v", username, member)

		usernames = append(usernames, username)
	}
	return usernames, nil
}

// updateOpenShiftGroup creates the OpenShift Group in etcd
func (s *LDAPGroupSyncer) updateOpenShiftGroup(openshiftGroup *userv1.Group) error {
	if len(openshiftGroup.UID) > 0 {
		_, err := s.GroupClient.Update(context.TODO(), openshiftGroup, metav1.UpdateOptions{})
		return err
	}

	_, err := s.GroupClient.Create(context.TODO(), openshiftGroup, metav1.CreateOptions{})
	return err
}

// makeOpenShiftGroup creates the OpenShift Group object that needs to be updated, updates its data
func (s *LDAPGroupSyncer) makeOpenShiftGroup(ldapGroupUID string, usernames []string) (*userv1.Group, error) {
	hostIP, _, err := net.SplitHostPort(s.Host)
	if err != nil {
		return nil, err
	}
	groupName, err := s.GroupNameMapper.GroupNameFor(ldapGroupUID)
	if err != nil {
		return nil, err
	}

	group, err := s.GroupClient.Get(context.TODO(), groupName, metav1.GetOptions{})
	if kapierrors.IsNotFound(err) {
		group = &userv1.Group{}
		group.Name = groupName
		group.Annotations = map[string]string{
			LDAPURLAnnotation: s.Host,
			LDAPUIDAnnotation: ldapGroupUID,
		}
		group.Labels = map[string]string{
			LDAPHostLabel: hostIP,
		}

	} else if err != nil {
		return nil, err
	}

	// make sure we aren't taking over an OpenShift group that is already related to a different LDAP group
	if host, exists := group.Labels[LDAPHostLabel]; !exists || (host != hostIP) {
		return nil, fmt.Errorf("group %q: %s label did not match sync host: wanted %s, got %s",
			group.Name, LDAPHostLabel, hostIP, host)
	}
	if url, exists := group.Annotations[LDAPURLAnnotation]; !exists || (url != s.Host) {
		return nil, fmt.Errorf("group %q: %s annotation did not match sync host: wanted %s, got %s",
			group.Name, LDAPURLAnnotation, s.Host, url)
	}
	if uid, exists := group.Annotations[LDAPUIDAnnotation]; !exists || (uid != ldapGroupUID) {
		return nil, fmt.Errorf("group %q: %s annotation did not match LDAP UID: wanted %s, got %s",
			group.Name, LDAPUIDAnnotation, ldapGroupUID, uid)
	}

	// overwrite Group Users data
	group.Users = usernames
	group.Annotations[LDAPSyncTimeAnnotation] = ISO8601(time.Now())

	return group, nil
}

// ISO8601 returns an ISO 6801 formatted string from a time.
func ISO8601(t time.Time) string {
	var tz string
	if zone, offset := t.Zone(); zone == "UTC" {
		tz = "Z"
	} else {
		tz = fmt.Sprintf("%03d00", offset/3600)
	}
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d%s",
		t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), tz)
}
