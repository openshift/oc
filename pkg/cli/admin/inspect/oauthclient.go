package inspect

import (
	"context"
	"fmt"
	"os"
	"path"

	oauthv1 "github.com/openshift/api/oauth/v1"
	"k8s.io/cli-runtime/pkg/resource"
)

var _ listAccessor = &oauthClientList{}

type oauthClientList struct {
	*oauthv1.OAuthClientList
}

func (c *oauthClientList) addItem(obj any) error {
	structuredItem, ok := obj.(*oauthv1.OAuthClient)
	if !ok {
		return fmt.Errorf("unhandledStructuredItemType: %T", obj)
	}
	c.Items = append(c.Items, *structuredItem)
	return nil
}

func inspectOAuthClientInfo(ctx context.Context, info *resource.Info, o *InspectOptions) error {
	structuredObj, err := toStructuredObject[oauthv1.OAuthClient, oauthv1.OAuthClientList](info.Object)
	if err != nil {
		return err
	}

	switch castObj := structuredObj.(type) {
	case *oauthv1.OAuthClient:
		elideOAuthClient(castObj)

	case *oauthv1.OAuthClientList:
		for i := range castObj.Items {
			elideOAuthClient(&castObj.Items[i])
		}
	}

	dirPath := dirPathForInfo(o.DestDir, info)
	filename := filenameForInfo(info)
	if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
		return err
	}
	return o.fileWriter.WriteFromResource(ctx, path.Join(dirPath, filename), structuredObj)
}

// elideOAuthClient replaces OAuthClient secret values with length stubs to prevent
// sensitive credential material from being written to must-gather output.
func elideOAuthClient(client *oauthv1.OAuthClient) {
	if len(client.Secret) > 0 {
		client.Secret = fmt.Sprintf("%d bytes long", len(client.Secret))
	}
	for i, s := range client.AdditionalSecrets {
		client.AdditionalSecrets[i] = fmt.Sprintf("%d bytes long", len(s))
	}
}
