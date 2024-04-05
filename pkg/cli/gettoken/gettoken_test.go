package gettoken

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/openshift/oc/pkg/cli/gettoken/credwriter"

	"golang.org/x/oauth2"

	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/openshift/oc/pkg/cli/gettoken/tokencache"
)

type FakeClient struct {
	authCodeExpire bool
	refreshExpire  bool
	verifyExpire   bool
}

func (c *FakeClient) GetTokenByAuthCode(ctx context.Context, callbackAddress string, localServerReadyChan chan<- string) (string, string, time.Time, error) {
	if c.authCodeExpire {
		return "", "", time.Now(), &oidc.TokenExpiredError{}
	}
	tm, err := time.Parse(time.DateTime, "2006-01-02 15:04:05")
	if err != nil {
		return "", "", time.Now(), err
	}
	return "test", "test", tm, nil
}

func (c *FakeClient) Refresh(ctx context.Context, refreshToken string) (string, string, time.Time, error) {
	if c.refreshExpire {
		return "", "", time.Now(), &oidc.TokenExpiredError{}
	}
	tm, err := time.Parse(time.DateTime, "2006-01-02 15:04:05")
	if err != nil {
		return "", "", time.Now(), err
	}
	return "test", refreshToken, tm, nil
}

func (c *FakeClient) VerifyToken(ctx context.Context, token *oauth2.Token, nonce string) (string, time.Time, error) {
	if c.verifyExpire {
		return "", time.Now(), &oidc.TokenExpiredError{}
	}
	tm, err := time.Parse(time.DateTime, "2006-01-02 15:04:05")
	if err != nil {
		return "", time.Now(), err
	}
	return "test", tm, nil
}

type FakeTokenCacher struct {
	cache map[tokencache.Key]tokencache.Set
}

func NewFakeTokenCacher() *FakeTokenCacher {
	cache := make(map[tokencache.Key]tokencache.Set)
	return &FakeTokenCacher{cache: cache}
}

func (f *FakeTokenCacher) FindByKey(dir string, key tokencache.Key) (*tokencache.Set, error) {
	if k, ok := f.cache[key]; ok {
		return &k, nil
	} else {
		return nil, nil
	}
}

func (f *FakeTokenCacher) Save(dir string, key tokencache.Key, tokenSet tokencache.Set) error {
	f.cache[key] = tokenSet
	return nil
}

func TestGetToken(t *testing.T) {
	streams, _, out, _ := genericiooptions.NewTestIOStreams()

	options := NewGetTokenOptions(streams)
	err := options.Validate()
	if err == nil || err.Error() != "--issuer-url is required" {
		t.Errorf("expected --issuer-url is required err %v", err)
	}
	options.IssuerURL = "test-issuer-url"
	err = options.Validate()
	if err == nil || err.Error() != "--client-id is required" {
		t.Errorf("expected --client-id is required error %v", err)
	}
	options.ClientID = "test-client-id"
	options.authenticator = &FakeClient{}
	options.tokenCache = NewFakeTokenCacher()
	options.credWriter = credwriter.NewWriter(streams)
	err = options.Run()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected := `{"kind":"ExecCredential","apiVersion":"client.authentication.k8s.io/v1","spec":{"interactive":false},"status":{"expirationTimestamp":"2006-01-02T15:04:05Z","token":"test"}}`
	expected = fmt.Sprintf("%s\n", expected)
	if out.String() != expected {
		t.Errorf("unexpected output %s expected %s", out.String(), expected)
	}
	value, err := options.tokenCache.FindByKey("", tokencache.Key{
		IssuerURL: "test-issuer-url",
		ClientID:  "test-client-id",
	})
	if err != nil {
		t.Errorf("unexpected error during cache retrieval %v", err)
	}
	if value == nil || value.IDToken != "test" || value.RefreshToken != "test" {
		t.Errorf("unexpected value returned from cache %v", value)
	}
	out.Reset()
	err = options.Run()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected = `{"kind":"ExecCredential","apiVersion":"client.authentication.k8s.io/v1","spec":{"interactive":false},"status":{"expirationTimestamp":"2006-01-02T15:04:05Z","token":"test"}}`
	expected = fmt.Sprintf("%s\n", expected)
	if out.String() != expected {
		t.Errorf("unexpected output %s expected %s", out.String(), expected)
	}

	options.authenticator = &FakeClient{verifyExpire: true}
	out.Reset()
	err = options.Run()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected = `{"kind":"ExecCredential","apiVersion":"client.authentication.k8s.io/v1","spec":{"interactive":false},"status":{"expirationTimestamp":"2006-01-02T15:04:05Z","token":"test"}}`
	expected = fmt.Sprintf("%s\n", expected)
	if out.String() != expected {
		t.Errorf("unexpected output %s expected %s", out.String(), expected)
	}

	options.authenticator = &FakeClient{verifyExpire: true, refreshExpire: true}
	out.Reset()
	err = options.Run()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	expected = `{"kind":"ExecCredential","apiVersion":"client.authentication.k8s.io/v1","spec":{"interactive":false},"status":{"expirationTimestamp":"2006-01-02T15:04:05Z","token":"test"}}`
	expected = fmt.Sprintf("%s\n", expected)
	if out.String() != expected {
		t.Errorf("unexpected output %s expected %s", out.String(), expected)
	}

	options.authenticator = &FakeClient{verifyExpire: true, refreshExpire: true, authCodeExpire: true}
	out.Reset()
	err = options.Run()
	tokenExpiredError := &oidc.TokenExpiredError{}
	if !errors.As(err, &tokenExpiredError) {
		t.Errorf("unexpected error %v", err)
	}
}
