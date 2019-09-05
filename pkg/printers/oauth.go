package printers

import (
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kprinters "k8s.io/kubernetes/pkg/printers"

	oauthv1 "github.com/openshift/api/oauth/v1"
)

func AddOAuthOpenShiftHandler(h kprinters.PrintHandler) {
	addOAuthClient(h)
	addOAuthAccessToken(h)
	addOAuthAuthorizeToken(h)
	addOAuthClientAuthorization(h)
}

func addOAuthClient(h kprinters.PrintHandler) {
	// oauthClientColumns              = []string{"Name", "Secret", "WWW-Challenge", "Token-Max-Age", "Redirect URIs"}
	oauthClientColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Secret", Type: "string", Description: oauthv1.OAuthClient{}.SwaggerDoc()["secret"]},
		{Name: "WWW-Challenge", Type: "bool", Description: oauthv1.OAuthClient{}.SwaggerDoc()["respondWithChallenges"]},
		{Name: "Token-Max-Age", Type: "string", Description: oauthv1.OAuthClient{}.SwaggerDoc()["accessTokenMaxAgeSeconds"]},
		{Name: "Redirect URIs", Type: "string", Description: oauthv1.OAuthClient{}.SwaggerDoc()["redirectURIs"]},
	}
	if err := h.TableHandler(oauthClientColumnsDefinitions, printOAuthClient); err != nil {
		panic(err)
	}
	if err := h.TableHandler(oauthClientColumnsDefinitions, printOAuthClientList); err != nil {
		panic(err)
	}
}

func printOAuthClient(oauthClient *oauthv1.OAuthClient, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: oauthClient},
	}

	name := formatResourceName(options.Kind, oauthClient.Name, options.WithKind)
	var maxAge string
	switch {
	case oauthClient.AccessTokenMaxAgeSeconds == nil:
		maxAge = "default"
	case *oauthClient.AccessTokenMaxAgeSeconds == 0:
		maxAge = "unexpiring"
	default:
		duration := time.Duration(*oauthClient.AccessTokenMaxAgeSeconds) * time.Second
		maxAge = duration.String()
	}

	row.Cells = append(row.Cells, name, oauthClient.Secret, oauthClient.RespondWithChallenges, maxAge, strings.Join(oauthClient.RedirectURIs, ","))

	return []metav1.TableRow{row}, nil
}

func printOAuthClientList(oauthClientList *oauthv1.OAuthClientList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(oauthClientList.Items))
	for i := range oauthClientList.Items {
		r, err := printOAuthClient(&oauthClientList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func addOAuthClientAuthorization(h kprinters.PrintHandler) {
	oauthClientColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "User Name", Type: "string", Format: "name", Description: oauthv1.OAuthClientAuthorization{}.SwaggerDoc()["userName"]},
		{Name: "Client Name", Type: "string", Format: "name", Description: oauthv1.OAuthClientAuthorization{}.SwaggerDoc()["clientName"]},
		{Name: "Scopes", Type: "string", Description: oauthv1.OAuthClientAuthorization{}.SwaggerDoc()["scopes"]},
	}
	if err := h.TableHandler(oauthClientColumnsDefinitions, printOAuthClientAuthorization); err != nil {
		panic(err)
	}
	if err := h.TableHandler(oauthClientColumnsDefinitions, printOAuthClientAuthorizationList); err != nil {
		panic(err)
	}
}

func printOAuthClientAuthorization(oauthClientAuthorization *oauthv1.OAuthClientAuthorization, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: oauthClientAuthorization},
	}
	row.Cells = append(row.Cells,
		formatResourceName(options.Kind, oauthClientAuthorization.Name, options.WithKind),
		oauthClientAuthorization.UserName,
		oauthClientAuthorization.ClientName,
		strings.Join(oauthClientAuthorization.Scopes, ","),
	)
	return []metav1.TableRow{row}, nil
}

func printOAuthClientAuthorizationList(oauthClientAuthorizationList *oauthv1.OAuthClientAuthorizationList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(oauthClientAuthorizationList.Items))
	for i := range oauthClientAuthorizationList.Items {
		r, err := printOAuthClientAuthorization(&oauthClientAuthorizationList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func addOAuthAccessToken(h kprinters.PrintHandler) {
	oauthClientColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "User Name", Type: "string", Format: "name", Description: oauthv1.OAuthAccessToken{}.SwaggerDoc()["userName"]},
		{Name: "Client Name", Type: "string", Format: "name", Description: oauthv1.OAuthAccessToken{}.SwaggerDoc()["clientName"]},
		{Name: "Created", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Expires", Type: "string", Description: oauthv1.OAuthAccessToken{}.SwaggerDoc()["expiresIn"]},
		{Name: "Redirect URI", Type: "string", Description: oauthv1.OAuthAccessToken{}.SwaggerDoc()["redirectURI"]},
		{Name: "Scopes", Type: "string", Description: oauthv1.OAuthAccessToken{}.SwaggerDoc()["scopes"]},
	}
	if err := h.TableHandler(oauthClientColumnsDefinitions, printOAuthAccessToken); err != nil {
		panic(err)
	}
	if err := h.TableHandler(oauthClientColumnsDefinitions, printOAuthAccessTokenList); err != nil {
		panic(err)
	}
}

func printOAuthAccessToken(oauthAccessToken *oauthv1.OAuthAccessToken, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: oauthAccessToken},
	}
	created := oauthAccessToken.CreationTimestamp
	expires := "never"
	if oauthAccessToken.ExpiresIn > 0 {
		expires = created.Add(time.Duration(oauthAccessToken.ExpiresIn) * time.Second).String()
	}
	row.Cells = append(row.Cells,
		formatResourceName(options.Kind, oauthAccessToken.Name, options.WithKind),
		oauthAccessToken.UserName,
		oauthAccessToken.ClientName,
		created,
		expires,
		oauthAccessToken.RedirectURI,
		strings.Join(oauthAccessToken.Scopes, ","),
	)
	return []metav1.TableRow{row}, nil
}

func printOAuthAccessTokenList(oauthAccessTokenList *oauthv1.OAuthAccessTokenList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(oauthAccessTokenList.Items))
	for i := range oauthAccessTokenList.Items {
		r, err := printOAuthAccessToken(&oauthAccessTokenList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func addOAuthAuthorizeToken(h kprinters.PrintHandler) {
	oauthClientColumnsDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "User Name", Type: "string", Format: "name", Description: oauthv1.OAuthAuthorizeToken{}.SwaggerDoc()["userName"]},
		{Name: "Client Name", Type: "string", Format: "name", Description: oauthv1.OAuthAuthorizeToken{}.SwaggerDoc()["userName"]},
		{Name: "Created", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Expires", Type: "string", Description: oauthv1.OAuthAuthorizeToken{}.SwaggerDoc()["expiresIn"]},
		{Name: "Redirect URI", Type: "string", Description: oauthv1.OAuthAuthorizeToken{}.SwaggerDoc()["redirectURI"]},
		{Name: "Scopes", Type: "string", Description: oauthv1.OAuthAuthorizeToken{}.SwaggerDoc()["scopes"]},
	}
	if err := h.TableHandler(oauthClientColumnsDefinitions, printOAuthAuthorizeToken); err != nil {
		panic(err)
	}
	if err := h.TableHandler(oauthClientColumnsDefinitions, printOAuthAuthorizeTokenList); err != nil {
		panic(err)
	}
}

func printOAuthAuthorizeToken(oauthAuthorizeToken *oauthv1.OAuthAuthorizeToken, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: oauthAuthorizeToken},
	}
	created := oauthAuthorizeToken.CreationTimestamp
	expires := "never"
	if oauthAuthorizeToken.ExpiresIn > 0 {
		expires = created.Add(time.Duration(oauthAuthorizeToken.ExpiresIn) * time.Second).String()
	}
	row.Cells = append(row.Cells,
		formatResourceName(options.Kind, oauthAuthorizeToken.Name, options.WithKind),
		oauthAuthorizeToken.UserName,
		oauthAuthorizeToken.ClientName,
		created,
		expires,
		oauthAuthorizeToken.RedirectURI,
		strings.Join(oauthAuthorizeToken.Scopes, ","),
	)
	return []metav1.TableRow{row}, nil
}

func printOAuthAuthorizeTokenList(oauthAuthorizeTokenList *oauthv1.OAuthAuthorizeTokenList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(oauthAuthorizeTokenList.Items))
	for i := range oauthAuthorizeTokenList.Items {
		r, err := printOAuthAuthorizeToken(&oauthAuthorizeTokenList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}
