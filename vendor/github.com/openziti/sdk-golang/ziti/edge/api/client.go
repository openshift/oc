package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/common/constants"
	"github.com/openziti/foundation/identity/identity"
	"github.com/openziti/sdk-golang/ziti/edge"
	"github.com/openziti/sdk-golang/ziti/sdkinfo"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type AuthFailure struct {
	httpCode int
	msg      string
}

func (e AuthFailure) Error() string {
	return fmt.Sprintf("authentication failed with http status code %v and msg: %v", e.httpCode, e.msg)
}

type notAuthorized struct{}

func (e notAuthorized) Error() string {
	return fmt.Sprintf("not authorized")
}

var NotAuthorized = notAuthorized{}

type NotAccessible struct {
	httpCode int
	msg      string
}

func (e NotAccessible) Error() string {
	return fmt.Sprintf("unable to create apiSession. http status code: %v, msg: %v", e.httpCode, e.msg)
}

type Errors struct {
	Errors []error
}

func (e Errors) Error() string {
	return fmt.Sprintf("%v", e.Errors)
}

type NotFound NotAccessible

func (e NotFound) Error() string {
	return fmt.Sprintf("unable to find resource. http status code: %v, msg: %v", e.httpCode, e.msg)
}

type RestClient interface {
	GetCurrentApiSession() *edge.ApiSession
	GetCurrentIdentity() (*edge.CurrentIdentity, error)
	Login(info map[string]interface{}) (*edge.ApiSession, error)
	Refresh() (*time.Time, error)
	GetServices() ([]*edge.Service, error)
	GetServiceTerminators(service *edge.Service, offset, limit int) ([]*edge.Terminator, int, error)
	IsServiceListUpdateAvailable() (bool, error)
	CreateSession(svcId string, kind edge.SessionType) (*edge.Session, error)
	RefreshSession(id string) (*edge.Session, error)
	SendPostureResponse(response PostureResponse) error
	SendPostureResponseBulk(responses []*PostureResponse) error

	AuthenticateMFA(code string) error
	VerifyMfa(code string) error
	EnrollMfa() (*MfaEnrollment, error)
	RemoveMfa(code string) error
	GenerateNewMfaRecoveryCodes(code string) error
	GetMfaRecoveryCodes(code string) ([]string, error)
	Shutdown()
}

type Client interface {
	Initialize() error
	GetIdentity() identity.Identity
	RestClient
}

func NewClient(ctrl *url.URL, tlsCfg *tls.Config, configTypes []string) (RestClient, error) {
	return &ctrlClient{
		zitiUrl: ctrl,
		clt: http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
			Timeout: 30 * time.Second,
		},
		configTypes: configTypes,
	}, nil
}

var authUrl, _ = url.Parse("/authenticate?method=cert")
var authMfaUrl, _ = url.Parse("/authenticate/mfa")
var currSess, _ = url.Parse("/current-api-session")
var currIdentity, _ = url.Parse("/current-identity")
var serviceUpdate, _ = url.Parse("/current-api-session/service-updates")
var servicesUrl, _ = url.Parse("/services")
var sessionUrl, _ = url.Parse("/sessions")
var postureResponseUrl, _ = url.Parse("/posture-response")
var postureResponseBulkUrl, _ = url.Parse("/posture-response-bulk")
var currentIdentityMfa, _ = url.Parse("/current-identity/mfa")
var currentIdentityMfaVerify, _ = url.Parse("/current-identity/mfa/verify")
var currentIdentityMfaRecoveryCodes, _ = url.Parse("/current-identity/mfa/recovery-codes")

type ctrlClient struct {
	configTypes        []string
	zitiUrl            *url.URL
	clt                http.Client
	apiSession         *edge.ApiSession
	lastServiceRefresh time.Time
	lastServiceUpdate  time.Time
}

func (c *ctrlClient) Shutdown() {
	c.clt.CloseIdleConnections()
}

func (c *ctrlClient) GetCurrentApiSession() *edge.ApiSession {
	return c.apiSession
}

func (c *ctrlClient) GetCurrentIdentity() (*edge.CurrentIdentity, error) {
	log := pfxlog.Logger()

	log.Debugf("getting current identity information")
	req, err := http.NewRequest("GET", c.zitiUrl.ResolveReference(currIdentity).String(), nil)

	if err != nil {
		return nil, errors.Wrap(err, "failed to create new HTTP request")
	}

	if c.apiSession == nil {
		return nil, errors.New("no apiSession to refresh")
	}

	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	resp, err := c.clt.Do(req)
	if err != nil && resp == nil {
		return nil, errors.Wrap(err, "failed contact controller")
	}

	if resp == nil {
		return nil, errors.New("controller returned empty respose")
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		currIdentity := &edge.CurrentIdentity{}
		if _, err = edge.ApiResponseDecode(currIdentity, resp.Body); err != nil {
			return nil, errors.Wrap(err, "failed to parse current identity")
		}
		return currIdentity, nil
	}

	return nil, fmt.Errorf("unhandled response from controller interogating sessions: %v - %v", resp.StatusCode, resp.Body)
}

func (c *ctrlClient) IsServiceListUpdateAvailable() (bool, error) {
	log := pfxlog.Logger()

	log.Debugf("checking if service list update is available")
	req, err := http.NewRequest("GET", c.zitiUrl.ResolveReference(serviceUpdate).String(), nil)

	if err != nil {
		return false, errors.Wrap(err, "failed to create new HTTP request")
	}

	if c.apiSession == nil {
		return false, errors.New("not authenticated")
	}

	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	resp, err := c.clt.Do(req)
	if err != nil && resp == nil {
		return false, errors.Wrap(err, "failed contact controller")
	}

	if resp == nil {
		return false, errors.New("controller returned empty respose")
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		serviceUpdates := &edge.ServiceUpdates{}
		_, err = edge.ApiResponseDecode(serviceUpdates, resp.Body)
		if err != nil {
			return false, errors.Wrap(err, "failed to parse service updates")
		}
		c.lastServiceUpdate = serviceUpdates.LastChangeAt
		updateAvailable := c.lastServiceUpdate.After(c.lastServiceRefresh)
		log.Debugf("service list update available: %v", updateAvailable)
		return updateAvailable, nil
	}

	body, _ := ioutil.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return false, NotFound{
			httpCode: resp.StatusCode,
			msg:      string(body),
		}
	}

	return false, fmt.Errorf("unhandled response from controller interogating sessions: %v - %v", resp.StatusCode, string(body))
}

func (c *ctrlClient) CreateSession(svcId string, kind edge.SessionType) (*edge.Session, error) {
	body := fmt.Sprintf(`{"serviceId":"%s", "type": "%s"}`, svcId, kind)
	reqBody := bytes.NewBufferString(body)

	fullSessionUrl := c.zitiUrl.ResolveReference(sessionUrl).String()
	pfxlog.Logger().Debugf("requesting session from %v", fullSessionUrl)
	req, _ := http.NewRequest("POST", fullSessionUrl, reqBody)
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	logrus.WithField("service_id", svcId).Debug("requesting session")
	resp, err := c.clt.Do(req)

	if err != nil {
		return nil, err
	}

	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	return decodeSession(resp)
}

func (c *ctrlClient) SendPostureResponseBulk(responses []*PostureResponse) error {
	if len(responses) == 0 {
		return nil
	}

	jsonBody, err := json.Marshal(responses)

	if err != nil {
		return err
	}
	fullUrl := c.zitiUrl.ResolveReference(postureResponseBulkUrl).String()
	req, _ := http.NewRequest(http.MethodPost, fullUrl, bytes.NewReader(jsonBody))
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err := c.clt.Do(req)

	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusCreated {
		if resp.StatusCode == http.StatusNotFound {
			return &NotFound{
				httpCode: resp.StatusCode,
				msg:      "the bulk posture response endpoint is not supported",
			}
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("recieved error during bulk posture response submission, could not read body: %v", err)
		}
		return fmt.Errorf("recieved error during bulk posture response submission: %v", string(body))
	}

	return nil
}

func (c *ctrlClient) SendPostureResponse(response PostureResponse) error {
	jsonBody, err := json.Marshal(response)

	if err != nil {
		return err
	}
	fullUrl := c.zitiUrl.ResolveReference(postureResponseUrl).String()
	req, _ := http.NewRequest(http.MethodPost, fullUrl, bytes.NewReader(jsonBody))
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err := c.clt.Do(req)

	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("recieved error during posture response submission, could not read body: %v", err)
		}
		return fmt.Errorf("recieved error during posture response submission: %v", string(body))
	}

	return nil
}

func (c *ctrlClient) RefreshSession(id string) (*edge.Session, error) {
	sessionLookupUrl, _ := url.Parse(fmt.Sprintf("/sessions/%v", id))
	sessionLookupUrlStr := c.zitiUrl.ResolveReference(sessionLookupUrl).String()
	pfxlog.Logger().Debugf("requesting session from %v", sessionLookupUrlStr)
	req, _ := http.NewRequest(http.MethodGet, sessionLookupUrlStr, nil)
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	logrus.WithField("sessionId", id).Debug("requesting session")
	resp, err := c.clt.Do(req)

	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeSession(resp)
}

func (c *ctrlClient) Login(info map[string]interface{}) (*edge.ApiSession, error) {
	req := new(bytes.Buffer)
	reqMap := make(map[string]interface{})
	for k, v := range info {
		reqMap[k] = v
	}

	if len(c.configTypes) > 0 {
		reqMap["configTypes"] = c.configTypes
	}

	if err := json.NewEncoder(req).Encode(reqMap); err != nil {
		return nil, err
	}
	resp, err := c.clt.Post(c.zitiUrl.ResolveReference(authUrl).String(), "application/json", req)
	if err != nil {
		pfxlog.Logger().Errorf("failure to post auth %+v", err)
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		msg, _ := ioutil.ReadAll(resp.Body)
		pfxlog.Logger().Errorf("failed to authenticate with Ziti controller, result status: %v, msg: %v", resp.StatusCode, string(msg))
		return nil, AuthFailure{
			httpCode: resp.StatusCode,
			msg:      string(msg),
		}
	}

	apiSessionResp := &edge.ApiSession{}

	_, err = edge.ApiResponseDecode(apiSessionResp, resp.Body)
	if err != nil {
		return nil, err
	}

	logrus.
		WithField("apiSession", apiSessionResp.Id).
		Debugf("logged in as %s/%s", apiSessionResp.Identity.Name, apiSessionResp.Identity.Id)

	c.apiSession = apiSessionResp
	return c.apiSession, nil

}

// During MFA enrollment, and initial TOTP code must be provided to enable MFA authentication. The code
// provided MUST NOT be a recovery code. Until MFA enrollment is verified, MFA authentication will not
// be required or possible. MFA Posture Checks may restrict service access until MFA is enrolled
// and authenticated.
func (c *ctrlClient) VerifyMfa(code string) error {
	body := NewMFACodeBody(code)
	reqBody := bytes.NewBuffer(body)

	mfaVerifyUrl := c.zitiUrl.ResolveReference(currentIdentityMfaVerify).String()
	pfxlog.Logger().Debugf("verifying MFA: POST %v", mfaVerifyUrl)

	req, _ := http.NewRequest("POST", mfaVerifyUrl, reqBody)
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err := c.clt.Do(req)

	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("invalid status code returned attempting to verify MFA for enrollment [%v], response body: %v", resp.StatusCode, respBody)
	}

	return nil
}

// During authentication/login additional MFA authentication queries may be provided. AuthenticateMFA allows
// the current identity for their current api session to attempt to pass MFA authentication.
func (c *ctrlClient) AuthenticateMFA(code string) error {
	body := NewMFACodeBody(code)
	reqBody := bytes.NewBuffer(body)

	mfaAuthUrl := c.zitiUrl.ResolveReference(authMfaUrl).String()
	pfxlog.Logger().Debugf("authenticating MFA: POST %v", mfaAuthUrl)

	req, _ := http.NewRequest("POST", mfaAuthUrl, reqBody)
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err := c.clt.Do(req)

	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("invalid status code returned attempting to authenticate MFA [%v], response body: %v", resp.StatusCode, string(respBody))
	}

	return nil
}

type MfaEnrollment struct {
	ProvisioningUrl string
	RecoveryCodes   []string
}

// Enroll in MFA. Will only succeed if the current identity of the current API session is not enrolled in MFA.
// If MFA is already enrolled and re-enrollment is desired first use RemoveMfa.
func (c *ctrlClient) EnrollMfa() (*MfaEnrollment, error) {
	body := `{}`
	reqBody := bytes.NewBufferString(body)

	mfaUrl := c.zitiUrl.ResolveReference(currentIdentityMfa).String()
	pfxlog.Logger().Debugf("enrolling in mfa: POST %v", mfaUrl)
	req, _ := http.NewRequest("POST", mfaUrl, reqBody)
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err := c.clt.Do(req)

	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("invalid status code returned attempting to start MFA enrollment [%v], response body: %v", resp.StatusCode, string(respBody))
	}

	req, _ = http.NewRequest("GET", mfaUrl, bytes.NewBuffer(nil))
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err = c.clt.Do(req)

	respBody, err := parseApiResponseEnvelope(resp)

	if err != nil {
		return nil, fmt.Errorf("error parsing API MFA response: %v", err)
	}

	mfaEnrollment := &MfaEnrollment{}

	provisioningUrlVal, ok := respBody.Data["provisioningUrl"]

	if !ok {
		return nil, errors.New("could not find provisioning URL in MFA response")
	}

	mfaEnrollment.ProvisioningUrl, ok = provisioningUrlVal.(string)

	if !ok {
		return nil, fmt.Errorf("could not read provisioning URL, expected string got %T", provisioningUrlVal)
	}

	codes, err := getMfaRecoveryCodes(respBody)

	if err != nil {
		return nil, err
	}

	mfaEnrollment.RecoveryCodes = codes

	return mfaEnrollment, nil
}

// Remove the previous enrolled MFA. A valid TOTP/recovery code is required. If MFA enrollment has not been completed
// an emty string can be used to remove the partially started MFA enrollment.
func (c *ctrlClient) RemoveMfa(code string) error {
	body := NewMFACodeBody(code)
	reqBody := bytes.NewBuffer(body)

	mfaUrl := c.zitiUrl.ResolveReference(currentIdentityMfa).String()
	pfxlog.Logger().Debugf("removing mfa: DELETE %v", mfaUrl)
	req, _ := http.NewRequest("DELETE", mfaUrl, reqBody)
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err := c.clt.Do(req)

	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("invalid status code returned attempting to remove MFA [%v], response body: %v", resp.StatusCode, string(respBody))
	}
	return nil
}

// Generate new MFA recovery codes. Required previous MFA enrollment and a valid TOTP code.
func (c *ctrlClient) GenerateNewMfaRecoveryCodes(code string) error {
	body := NewMFACodeBody(code)
	reqBody := bytes.NewBuffer(body)

	mfaRecoveryCodesUrl := c.zitiUrl.ResolveReference(currentIdentityMfaRecoveryCodes).String()
	pfxlog.Logger().Debugf("generating new MFA recovery codes: POST %v", mfaRecoveryCodesUrl)
	req, _ := http.NewRequest("POST", mfaRecoveryCodesUrl, reqBody)
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err := c.clt.Do(req)

	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("invalid status code returned attempting to generate new MFA recovery codes [%v], response body: %v", resp.StatusCode, string(respBody))
	}

	return nil
}

// Retrieve MFA recovery codes. Required previous MFA enrollment and a valid TOTP code.
func (c *ctrlClient) GetMfaRecoveryCodes(code string) ([]string, error) {
	body := NewMFACodeBody(code)
	reqBody := bytes.NewBuffer(body)

	mfaRecoveryCodesUrl := c.zitiUrl.ResolveReference(currentIdentityMfaRecoveryCodes).String()
	pfxlog.Logger().Debugf("retrieving MFA recovery codes: GET %v", mfaRecoveryCodesUrl)
	req, _ := http.NewRequest("GET", mfaRecoveryCodesUrl, reqBody)
	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	req.Header.Set("content-type", "application/json")

	resp, err := c.clt.Do(req)

	if err != nil {
		return nil, fmt.Errorf("error requesting MFA recovery codes: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("could not retrieve MFA recovery codes [%v], response body: %v", resp.StatusCode, string(respBody))
	}

	respBody, err := parseApiResponseEnvelope(resp)

	if err != nil {
		return nil, fmt.Errorf("error parsing API MFA recovery code response: %v", err)
	}

	return getMfaRecoveryCodes(respBody)
}

func getMfaRecoveryCodes(respBody *ApiEnvelope) ([]string, error) {
	codeVal := respBody.Data["recoveryCodes"]

	if codeVal == nil {
		return nil, fmt.Errorf("unexpected nil value for recoveryCodes")
	}

	codeArr := codeVal.([]interface{})

	if codeArr == nil {
		return nil, fmt.Errorf("unexpected nil value for recoveryCodes as array")
	}

	var codes []string

	for _, codeVal := range codeArr {
		code, ok := codeVal.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected non string value found in recoveryCodes")
		}
		codes = append(codes, code)
	}

	return codes, nil
}

type ApiEnvelope struct {
	Meta map[string]interface{} `json:"meta"`
	Data map[string]interface{} `json:"data"`
}

func parseApiResponseEnvelope(resp *http.Response) (*ApiEnvelope, error) {
	respBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	respEnvelope := &ApiEnvelope{}
	json.Unmarshal(respBody, &respEnvelope)

	if respEnvelope.Data == nil {
		return nil, fmt.Errorf("data section of response not found for MFA recovery codes")
	}

	if respEnvelope.Meta == nil {
		return nil, fmt.Errorf("meta section of response not found for: %v %v", resp.Request.Method, resp.Request.URL.String())
	}

	return respEnvelope, nil
}

func (c *ctrlClient) Refresh() (*time.Time, error) {
	log := pfxlog.Logger()

	log.Debugf("refreshing apiSession")
	req, err := http.NewRequest("GET", c.zitiUrl.ResolveReference(currSess).String(), nil)

	if err != nil {
		return nil, fmt.Errorf("failed to create new HTTP request during refresh: %v", err)
	}

	if c.apiSession == nil {
		return nil, fmt.Errorf("no apiSession to refresh")
	}

	req.Header.Set(constants.ZitiSession, c.apiSession.Token)
	resp, err := c.clt.Do(req)
	if err != nil && resp == nil {
		return nil, fmt.Errorf("failed contact controller: %v", err)
	}

	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
			apiSessionResp := &edge.ApiSession{}
			_, err = edge.ApiResponseDecode(apiSessionResp, resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to parse current apiSession during refresh: %v", err)
			}
			c.apiSession = apiSessionResp
			log.Debugf("apiSession refreshed, new expiration[%s]", c.apiSession.Expires)
		} else if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
			log.Errorf("session is invalid, trying to login again: %+v", err)
			apiSession, err := c.Login(sdkinfo.GetSdkInfo())
			if err != nil {
				return nil, fmt.Errorf("failed to login during apiSession refresh: %v", err)
			}
			log.Debugf("new apiSession created, expiration[%s]", c.apiSession.Expires)
			c.apiSession = apiSession
		} else {
			return nil, fmt.Errorf("unhandled response from controller interogating sessions: %v - %v", resp.StatusCode, resp.Body)
		}

	}

	return &c.apiSession.Expires, nil
}

func (c *ctrlClient) GetServices() ([]*edge.Service, error) {
	servReq, _ := http.NewRequest("GET", c.zitiUrl.ResolveReference(servicesUrl).String(), nil)

	if c.apiSession.Token == "" {
		return nil, errors.New("apiSession apiSession token is empty")
	} else {
		pfxlog.Logger().Debugf("using apiSession apiSession token %v", c.apiSession.Token)
	}
	servReq.Header.Set(constants.ZitiSession, c.apiSession.Token)
	pgOffset := 0
	pgLimit := 500

	var services []*edge.Service
	for {
		q := servReq.URL.Query()
		q.Set("limit", strconv.Itoa(pgLimit))
		q.Set("offset", strconv.Itoa(pgOffset))
		servReq.URL.RawQuery = q.Encode()
		resp, err := c.clt.Do(servReq)

		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			if body, err := ioutil.ReadAll(resp.Body); err != nil {
				pfxlog.Logger().Debugf("error response: %v", body)
			}
			_ = resp.Body.Close()
			return nil, errors.New("unauthorized")
		}

		if err != nil {
			if resp != nil {
				_ = resp.Body.Close()
			}
			return nil, err
		}

		s := &[]*edge.Service{}
		meta, err := edge.ApiResponseDecode(s, resp.Body)

		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if meta == nil {
			// shouldn't happen
			return nil, errors.New("nil metadata in response to GET /services")
		}
		if meta.Pagination == nil {
			return nil, errors.New("nil pagination in response to GET /services")
		}

		if services == nil {
			services = make([]*edge.Service, 0, meta.Pagination.TotalCount)
		}

		for _, svc := range *s {
			services = append(services, svc)
		}

		pgOffset += pgLimit
		if pgOffset >= meta.Pagination.TotalCount {
			break
		}
	}

	c.lastServiceRefresh = c.lastServiceUpdate
	return services, nil
}

func (c *ctrlClient) GetServiceTerminators(service *edge.Service, offset, limit int) ([]*edge.Terminator, int, error) {
	terminatorsUrl, err := url.Parse(fmt.Sprintf("/services/%v/terminators", service.Id))
	if err != nil {
		return nil, 0, err
	}
	servReq, _ := http.NewRequest("GET", c.zitiUrl.ResolveReference(terminatorsUrl).String(), nil)

	if c.apiSession.Token == "" {
		return nil, 0, errors.New("apiSession apiSession token is empty")
	} else {
		pfxlog.Logger().Debugf("using apiSession apiSession token %v", c.apiSession.Token)
	}
	servReq.Header.Set(constants.ZitiSession, c.apiSession.Token)

	var terminators []*edge.Terminator
	q := servReq.URL.Query()
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(offset))
	servReq.URL.RawQuery = q.Encode()
	resp, err := c.clt.Do(servReq)

	if resp != nil && resp.StatusCode == http.StatusUnauthorized {
		if body, err := ioutil.ReadAll(resp.Body); err != nil {
			pfxlog.Logger().Debugf("error response: %v", body)
		}
		_ = resp.Body.Close()
		return nil, 0, errors.New("unauthorized")
	}

	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, 0, err
	}

	terminator := &[]*edge.Terminator{}
	meta, err := edge.ApiResponseDecode(terminator, resp.Body)

	_ = resp.Body.Close()
	if err != nil {
		return nil, 0, err
	}
	if meta == nil {
		// shouldn't happen
		return nil, 0, errors.Errorf("nil metadata in response to GET /services/%v/terminators", service.Id)
	}
	if meta.Pagination == nil {
		return nil, 0, errors.Errorf("nil pagination in response to GET /services/%v/terminators", service.Id)
	}

	if terminators == nil {
		terminators = make([]*edge.Terminator, 0, meta.Pagination.TotalCount)
	}

	for _, svc := range *terminator {
		terminators = append(terminators, svc)
	}

	return terminators, meta.Pagination.TotalCount, nil
}

func decodeSession(resp *http.Response) (*edge.Session, error) {
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		respBody, _ := ioutil.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, NotAuthorized
		}
		if resp.StatusCode == http.StatusBadRequest {
			return nil, NotAccessible{
				httpCode: resp.StatusCode,
				msg:      string(respBody),
			}
		}

		if resp.StatusCode == http.StatusNotFound {
			return nil, NotFound{
				httpCode: resp.StatusCode,
				msg:      string(respBody),
			}
		}
		return nil, fmt.Errorf("failed to create session: %s\n%s", resp.Status, string(respBody))
	}

	session := new(edge.Session)
	_, err := edge.ApiResponseDecode(session, resp.Body)
	if err != nil {
		pfxlog.Logger().WithError(err).Error("failed to decode session response")
		return nil, err
	}

	for _, edgeRouter := range session.EdgeRouters {
		delete(edgeRouter.Urls, "wss")
		delete(edgeRouter.Urls, "ws")
	}

	return session, nil
}

type PostureResponse struct {
	Id             string `json:"id"`
	TypeId         string `json:"typeId"`
	PostureSubType `json:"-"`
}

func (response PostureResponse) MarshalJSON() ([]byte, error) {
	type alias PostureResponse

	respJson, err := json.Marshal(alias(response))
	if err != nil {
		return nil, err
	}

	subType, err := json.Marshal(response.PostureSubType)
	if err != nil {
		return nil, err
	}

	s1 := string(respJson[:len(respJson)-1])
	s2 := string(subType[1:])

	return []byte(s1 + ", " + s2), nil
}

const (
	PostureCheckTypeOs      = "OS"
	PostureCheckTypeDomain  = "DOMAIN"
	PostureCheckTypeProcess = "PROCESS"
	PostureCheckTypeMAC     = "MAC"
)

type PostureSubType interface {
	IsPostureSubType()
}

type PostureResponseOs struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Build   string `json:"build"`
}

func (p PostureResponseOs) IsPostureSubType() {}

type PostureResponseDomain struct {
	Domain string `json:"domain"`
}

func (p PostureResponseDomain) IsPostureSubType() {}

type PostureResponseMac struct {
	MacAddresses []string `json:"macAddresses"`
}

func (p PostureResponseMac) IsPostureSubType() {}

type PostureResponseProcess struct {
	IsRunning          bool     `json:"isRunning"`
	Hash               string   `json:"hash"`
	SignerFingerprints []string `json:"signerFingerprints"`
}

func (p PostureResponseProcess) IsPostureSubType() {}

type MFACode struct {
	Code string `json: "code"`
}

func NewMFACodeBody(code string) []byte {
	val := MFACode{
		Code: code,
	}
	body, _ := json.Marshal(val)
	return body
}
