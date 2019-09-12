package ldapquery

import (
	"fmt"
	"strings"

	"gopkg.in/ldap.v2"
	"k8s.io/klog"

	"github.com/openshift/library-go/pkg/security/ldapclient"
	"github.com/openshift/library-go/pkg/security/ldaputil"
)

// NewLDAPQuery converts a user-provided LDAPQuery into a version we can use
func NewLDAPQuery(config SerializeableLDAPQuery) (LDAPQuery, error) {
	scope, err := ldaputil.DetermineLDAPScope(config.Scope)
	if err != nil {
		return LDAPQuery{}, err
	}

	derefAliases, err := ldaputil.DetermineDerefAliasesBehavior(config.DerefAliases)
	if err != nil {
		return LDAPQuery{}, err
	}

	return LDAPQuery{
		BaseDN:       config.BaseDN,
		Scope:        scope,
		DerefAliases: derefAliases,
		TimeLimit:    config.TimeLimit,
		Filter:       config.Filter,
		PageSize:     config.PageSize,
	}, nil
}

// LDAPQuery encodes an LDAP query
type LDAPQuery struct {
	// The DN of the branch of the directory where all searches should start from
	BaseDN string

	// The (optional) scope of the search. Defaults to the entire subtree if not set
	Scope ldaputil.Scope

	// The (optional) behavior of the search with regards to alisases. Defaults to always
	// dereferencing if not set
	DerefAliases ldaputil.DerefAliases

	// TimeLimit holds the limit of time in seconds that any request to the server can remain outstanding
	// before the wait for a response is given up. If this is 0, no client-side limit is imposed
	TimeLimit int

	// Filter is a valid LDAP search filter that retrieves all relevant entries from the LDAP server with the base DN
	Filter string

	// PageSize is the maximum preferred page size, measured in LDAP entries. A page size of 0 means no paging will be done.
	PageSize int
}

// NewSearchRequest creates a new search request for the LDAP query and optionally includes more attributes
func (q *LDAPQuery) NewSearchRequest(additionalAttributes []string) *ldap.SearchRequest {
	var controls []ldap.Control
	if q.PageSize > 0 {
		controls = append(controls, ldap.NewControlPaging(uint32(q.PageSize)))
	}
	return ldap.NewSearchRequest(
		q.BaseDN,
		int(q.Scope),
		int(q.DerefAliases),
		0, // allowed return size - indicates no limit
		q.TimeLimit,
		false, // not types only
		q.Filter,
		additionalAttributes,
		controls,
	)
}

// NewLDAPQueryOnAttribute converts a user-provided LDAPQuery into a version we can use by parsing
// the input and combining it with a set of name attributes
func NewLDAPQueryOnAttribute(config SerializeableLDAPQuery, attribute string) (LDAPQueryOnAttribute, error) {
	ldapQuery, err := NewLDAPQuery(config)
	if err != nil {
		return LDAPQueryOnAttribute{}, err
	}

	return LDAPQueryOnAttribute{
		LDAPQuery:      ldapQuery,
		QueryAttribute: attribute,
	}, nil
}

// LDAPQueryOnAttribute encodes an LDAP query that conjoins two filters to extract a specific LDAP entry
// This query is not self-sufficient and needs the value of the QueryAttribute to construct the final filter
type LDAPQueryOnAttribute struct {
	// Query retrieves entries from an LDAP server
	LDAPQuery

	// QueryAttribute is the attribute for a specific filter that, when conjoined with the common filter,
	// retrieves the specific LDAP entry from the LDAP server. (e.g. "cn", when formatted with "aGroupName"
	// and conjoined with "objectClass=groupOfNames", becomes (&(objectClass=groupOfNames)(cn=aGroupName))")
	QueryAttribute string
}

// NewSearchRequest creates a new search request from the identifying query by internalizing the value of
// the attribute to be filtered as well as any attributes that need to be recovered
func (o *LDAPQueryOnAttribute) NewSearchRequest(attributeValue string, attributes []string) (*ldap.SearchRequest, error) {
	if strings.EqualFold(o.QueryAttribute, "dn") {
		dn, err := ldap.ParseDN(attributeValue)
		if err != nil {
			return nil, fmt.Errorf("could not search by dn, invalid dn value: %v", err)
		}
		baseDN, err := ldap.ParseDN(o.BaseDN)
		if err != nil {
			return nil, fmt.Errorf("could not search by dn, invalid dn value: %v", err)
		}
		if !baseDN.AncestorOf(dn) && !baseDN.Equal(dn) {
			return nil, NewQueryOutOfBoundsError(attributeValue, o.BaseDN)
		}
		return o.buildDNQuery(attributeValue, attributes), nil

	} else {
		return o.buildAttributeQuery(attributeValue, attributes), nil
	}
}

// buildDNQuery builds the query that finds an LDAP entry with the given DN
// this is done by setting the DN to be the base DN for the search and setting the search scope
// to only consider the base object found
func (o *LDAPQueryOnAttribute) buildDNQuery(dn string, attributes []string) *ldap.SearchRequest {
	var controls []ldap.Control
	if o.PageSize > 0 {
		controls = append(controls, ldap.NewControlPaging(uint32(o.PageSize)))
	}
	return ldap.NewSearchRequest(
		dn,
		ldap.ScopeBaseObject, // over-ride original
		int(o.DerefAliases),
		0, // allowed return size - indicates no limit
		o.TimeLimit,
		false,             // not types only
		"(objectClass=*)", // filter that returns all values
		attributes,
		controls,
	)
}

// buildAttributeQuery builds the query containing a filter that conjoins the common filter given
// in the configuration with the specific attribute filter for which the attribute value is given
func (o *LDAPQueryOnAttribute) buildAttributeQuery(attributeValue string,
	attributes []string) *ldap.SearchRequest {
	specificFilter := fmt.Sprintf("%s=%s",
		ldap.EscapeFilter(o.QueryAttribute),
		ldap.EscapeFilter(attributeValue))

	filter := fmt.Sprintf("(&(%s)(%s))", o.Filter, specificFilter)

	var controls []ldap.Control
	if o.PageSize > 0 {
		controls = append(controls, ldap.NewControlPaging(uint32(o.PageSize)))
	}

	return ldap.NewSearchRequest(
		o.BaseDN,
		int(o.Scope),
		int(o.DerefAliases),
		0, // allowed return size - indicates no limit
		o.TimeLimit,
		false, // not types only
		filter,
		attributes,
		controls,
	)
}

// QueryForUniqueEntry queries for an LDAP entry with the given searchRequest. The query is expected
// to return one unqiue result. If this is not the case, errors are raised
func QueryForUniqueEntry(clientConfig ldapclient.Config, query *ldap.SearchRequest) (*ldap.Entry, error) {
	result, err := QueryForEntries(clientConfig, query)
	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return nil, NewEntryNotFoundError(query.BaseDN, query.Filter)
	}

	if len(result) > 1 {
		if query.Scope == ldap.ScopeBaseObject {
			return nil, fmt.Errorf("multiple entries found matching dn=%q:\n%s",
				query.BaseDN, formatResult(result))
		} else {
			return nil, fmt.Errorf("multiple entries found matching filter %s:\n%s",
				query.Filter, formatResult(result))
		}
	}

	entry := result[0]
	klog.V(4).Infof("found dn=%q for %s", entry.DN, query.Filter)
	return entry, nil
}

// formatResult pretty-prints the first ten DNs in the slice of entries
func formatResult(results []*ldap.Entry) string {
	var names []string
	for _, entry := range results {
		names = append(names, entry.DN)
	}
	return "\t" + strings.Join(names[0:10], "\n\t")
}

// QueryForEntries queries for LDAP with the given searchRequest
func QueryForEntries(clientConfig ldapclient.Config, query *ldap.SearchRequest) ([]*ldap.Entry, error) {
	connection, err := clientConfig.Connect()
	if err != nil {
		return nil, fmt.Errorf("could not connect to the LDAP server: %v", err)
	}
	defer connection.Close()

	if bindDN, bindPassword := clientConfig.GetBindCredentials(); len(bindDN) > 0 {
		if err := connection.Bind(bindDN, bindPassword); err != nil {
			return nil, fmt.Errorf("could not bind to the LDAP server: %v", err)
		}
	}

	var searchResult *ldap.SearchResult
	control := ldap.FindControl(query.Controls, ldap.ControlTypePaging)
	if control == nil {
		klog.V(4).Infof("searching LDAP server with config %v with dn=%q and scope %v for %s requesting %v", clientConfig, query.BaseDN, query.Scope, query.Filter, query.Attributes)
		searchResult, err = connection.Search(query)
	} else if pagingControl, ok := control.(*ldap.ControlPaging); ok {
		klog.V(4).Infof("searching LDAP server with config %v with dn=%q and scope %v for %s requesting %v with pageSize=%d", clientConfig, query.BaseDN, query.Scope, query.Filter, query.Attributes, pagingControl.PagingSize)
		searchResult, err = connection.SearchWithPaging(query, pagingControl.PagingSize)
	} else {
		err = fmt.Errorf("invalid paging control type: %v", control)
	}

	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return nil, NewNoSuchObjectError(query.BaseDN)
		}
		return nil, err
	}

	for _, entry := range searchResult.Entries {
		klog.V(4).Infof("found dn=%q ", entry.DN)
	}
	return searchResult.Entries, nil
}
