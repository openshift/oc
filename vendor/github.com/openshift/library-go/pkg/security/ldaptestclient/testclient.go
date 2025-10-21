package ldaptestclient

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// Fake is a mock client for an LDAP server
// The following methods define safe defaults for the return values. In order to adapt this test client
// for a specific test, anonymously include it and override the method being tested. In the over-riden
// method, if you are not covering all method calls with your override, defer to the parent for handling.
type Fake struct {
	SimpleBindResponse     *ldap.SimpleBindResult
	PasswordModifyResponse *ldap.PasswordModifyResult
	SearchResponse         *ldap.SearchResult
}

var _ ldap.Client = &Fake{}

// NewTestClient returns a new test client with safe default return values
func New() *Fake {
	return &Fake{
		SimpleBindResponse: &ldap.SimpleBindResult{
			Controls: []ldap.Control{},
		},
		PasswordModifyResponse: &ldap.PasswordModifyResult{
			GeneratedPassword: "",
		},
		SearchResponse: &ldap.SearchResult{
			Entries:   []*ldap.Entry{},
			Referrals: []string{},
			Controls:  []ldap.Control{},
		},
	}
}

// Start starts the LDAP connection
func (c *Fake) Start() {
	return
}

// StartTLS begins a TLS-wrapped LDAP connection
func (c *Fake) StartTLS(config *tls.Config) error {
	return nil
}

// Close closes an LDAP connection
func (c *Fake) Close() error {
	return nil
}

// GetLastError returns the last recorded error.
func (c *Fake) GetLastError() error {
	return nil
}

// TLSConnectionState returns the client's TLS connection state.
func (c *Fake) TLSConnectionState() (tls.ConnectionState, bool) {
	return tls.ConnectionState{}, false
}

// Bind binds to the LDAP server with a bind DN and password
func (c *Fake) Bind(username, password string) error {
	return nil
}

// SimpleBind binds to the LDAP server using the Simple Bind mechanism
func (c *Fake) SimpleBind(simpleBindRequest *ldap.SimpleBindRequest) (*ldap.SimpleBindResult, error) {
	return c.SimpleBindResponse, nil
}

// NTLMUnauthenticatedBind performs a bind with an empty password.
func (c *Fake) NTLMUnauthenticatedBind(domain, username string) error {
	return nil
}

// Unbind will perform an unbind request.
func (c *Fake) Unbind() error {
	return nil
}

// Add forwards an addition request to the LDAP server
func (c *Fake) Add(addRequest *ldap.AddRequest) error {
	return nil
}

// Del forwards a deletion request to the LDAP server
func (c *Fake) Del(delRequest *ldap.DelRequest) error {
	return nil
}

// Modify forwards a modification request to the LDAP server
func (c *Fake) Modify(modifyRequest *ldap.ModifyRequest) error {
	return nil
}

// Compare ... ?
func (c *Fake) Compare(dn, attribute, value string) (bool, error) {
	return false, nil
}

// PasswordModify forwards a password modify request to the LDAP server
func (c *Fake) PasswordModify(passwordModifyRequest *ldap.PasswordModifyRequest) (*ldap.PasswordModifyResult, error) {
	return c.PasswordModifyResponse, nil
}

// Search forwards a search request to the LDAP server
func (c *Fake) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	return c.SearchResponse, nil
}

// SearchAsync performs a search request and returns all search results asynchronously.
func (c *Fake) SearchAsync(ctx context.Context, searchRequest *ldap.SearchRequest, bufferSize int) ldap.Response {
	return nil
}

// SearchWithPaging forwards a search request to the LDAP server and pages the response
func (c *Fake) SearchWithPaging(searchRequest *ldap.SearchRequest, pagingSize uint32) (*ldap.SearchResult, error) {
	return c.SearchResponse, nil
}

// DirSync does a Search with dirSync Control.
func (c *Fake) DirSync(searchRequest *ldap.SearchRequest, flags, maxAttrCount int64, cookie []byte) (*ldap.SearchResult, error) {
	return c.SearchResponse, nil
}

// DirSyncAsync performs a search request and returns all search results asynchronously.
func (c *Fake) DirSyncAsync(ctx context.Context, searchRequest *ldap.SearchRequest, bufferSize int, flags, maxAttrCount int64, cookie []byte) ldap.Response {
	return nil
}

// Syncrepl is a short name for LDAP Sync Replication engine that works on the consumer-side.
// This can perform a persistent search and returns an entry when the entry is updated on the server side.
func (c *Fake) Syncrepl(ctx context.Context, searchRequest *ldap.SearchRequest, bufferSize int, mode ldap.ControlSyncRequestMode, cookie []byte, reloadHint bool) ldap.Response {
	return nil
}

// SetTimeout sets a timeout on the client
func (c *Fake) SetTimeout(d time.Duration) {
}

func (c *Fake) IsClosing() bool {
	return false
}

func (c *Fake) UnauthenticatedBind(username string) error {
	return nil
}

func (c *Fake) ExternalBind() error {
	return nil
}

func (c *Fake) ModifyDN(request *ldap.ModifyDNRequest) error {
	return nil
}

func (c *Fake) ModifyWithResult(request *ldap.ModifyRequest) (*ldap.ModifyResult, error) {
	return nil, nil
}

// Extended performs an extended request.
func (c *Fake) Extended(request *ldap.ExtendedRequest) (*ldap.ExtendedResponse, error) {
	return nil, nil
}

// NewMatchingSearchErrorClient returns a new MatchingSearchError client sitting on top of the parent
// client. This client returns the given error when a search base DN matches the given base DN, and
// defers to the parent otherwise.
func NewMatchingSearchErrorClient(parent ldap.Client, baseDN string, returnErr error) ldap.Client {
	return &MatchingSearchErrClient{
		Client:    parent,
		BaseDN:    baseDN,
		ReturnErr: returnErr,
	}
}

// MatchingSearchErrClient returns the ReturnErr on every Search() where the search base DN matches the given DN
// or defers the search to the parent client
type MatchingSearchErrClient struct {
	ldap.Client
	BaseDN    string
	ReturnErr error
}

func (c *MatchingSearchErrClient) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	if searchRequest.BaseDN == c.BaseDN {
		return nil, c.ReturnErr
	}
	return c.Client.Search(searchRequest)
}

// NewDNMappingClient returns a new DNMappingClient sitting on top of the parent client. This client returns the
// ldap entries mapped to with this DN in its' internal DN map, or defers to the parent if the DN is not mapped.
func NewDNMappingClient(parent ldap.Client, DNMapping map[string][]*ldap.Entry) ldap.Client {
	return &DNMappingClient{
		Client:    parent,
		DNMapping: DNMapping,
	}
}

// DNMappingClient returns the LDAP entry mapped to by the base dn given, or if no mapping happens, defers to the parent
type DNMappingClient struct {
	ldap.Client
	DNMapping map[string][]*ldap.Entry
}

func (c *DNMappingClient) Search(searchRequest *ldap.SearchRequest) (*ldap.SearchResult, error) {
	if entries, exists := c.DNMapping[searchRequest.BaseDN]; exists {
		return &ldap.SearchResult{Entries: entries}, nil
	}

	return c.Client.Search(searchRequest)
}

// NewPagingOnlyClient returns a new PagingOnlyClient sitting on top of the parent client. This client returns the
// provided search response for any calls to SearchWithPaging, or defers to the parent if the call is not to the
// paged search function.
func NewPagingOnlyClient(parent ldap.Client, response *ldap.SearchResult) ldap.Client {
	return &PagingOnlyClient{
		Client:   parent,
		Response: response,
	}
}

// PagingOnlyClient responds with a canned search result for any calls to SearchWithPaging
type PagingOnlyClient struct {
	ldap.Client
	Response *ldap.SearchResult
}

func (c *PagingOnlyClient) SearchWithPaging(searchRequest *ldap.SearchRequest, pagingSize uint32) (*ldap.SearchResult, error) {
	return c.Response, nil
}
