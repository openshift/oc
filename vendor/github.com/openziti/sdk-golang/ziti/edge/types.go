/*
	Copyright 2019 NetFoundry, Inc.

	Licensed under the Apache License, Version 2.0 (the "License");
	you may not use this file except in compliance with the License.
	You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

	Unless required by applicable law or agreed to in writing, software
	distributed under the License is distributed on an "AS IS" BASIS,
	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
	See the License for the specific language governing permissions and
	limitations under the License.
*/

package edge

import (
	"encoding/json"
	"github.com/michaelquigley/pfxlog"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"io"
	"time"
)

type CurrentIdentity struct {
	Id                        string                 `json:"id"`
	Name                      string                 `json:"name"`
	AppData                   map[string]interface{} `json:"appData"`
	DefaultHostingPrecedence  string                 `json:"defaultHostingPrecedence"`
	DefaultHostingCost        uint16                 `json:"defaultHostingCost"`
	ServiceHostingPrecedences map[string]interface{} `json:"serviceHostingPrecedences"`
	ServiceHostingCosts       map[string]interface{} `json:"serviceHostingCosts"`
}

type ApiIdentity struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type ApiSession struct {
	Id          string       `json:"id"`
	Token       string       `json:"token"`
	Identity    *ApiIdentity `json:"identity"`
	Expires     time.Time    `json:"expiresAt"`
	AuthQueries []*AuthQuery `json:"authQueries"`
}

type AuthQuery struct {
	Format     string `json:"format,omitempty"`
	HTTPMethod string `json:"httpMethod,omitempty"`
	HTTPURL    string `json:"httpUrl,omitempty"`
	MaxLength  int64  `json:"maxLength,omitempty"`
	MinLength  int64  `json:"minLength,omitempty"`
	Provider   string `json:"provider"`
}

type ServiceUpdates struct {
	LastChangeAt time.Time `json:"lastChangeAt"`
}

type EdgeRouter struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Urls     map[string]string
}

type SessionType string

const (
	SessionDial SessionType = "Dial"
	SessionBind SessionType = "Bind"
)

type Session struct {
	Id          string       `json:"id"`
	Service     ApiIdentity  `json:"service"`
	Token       string       `json:"token"`
	Type        SessionType  `json:"type"`
	EdgeRouters []EdgeRouter `json:"edgeRouters"`
}

type Service struct {
	Id             string                            `json:"id"`
	Name           string                            `json:"name"`
	Permissions    []string                          `json:"permissions"`
	Encryption     bool                              `json:"encryptionRequired"`
	PostureQueries []PostureQueries                  `json:"postureQueries"`
	Configs        map[string]map[string]interface{} `json:"config"`
	Tags           map[string]string                 `json:"tags"`
}

type Terminator struct {
	Id        string `json:"id"`
	ServiceId string `json:"serviceId"`
	RouterId  string `json:"routerId"`
	Identity  string `json:"Identity"`
}

type PostureQueries struct {
	IsPassing      bool `json:"isPassing"`
	PostureQueries []PostureQuery
}

type PostureQuery struct {
	Id        string               `json:"id"`
	IsPassing bool                 `json:"isPassing"`
	QueryType string               `json:"queryType"`
	Process   *PostureQueryProcess `json:"process"`
}

type PostureQueryProcess struct {
	OsType string `json:"osType"`
	Path   string `json:"path"`
}

func (service *Service) GetConfigOfType(configType string, target interface{}) (bool, error) {
	if service.Configs == nil {
		pfxlog.Logger().Debugf("no service configs defined for service %v", service.Name)
		return false, nil
	}
	configMap, found := service.Configs[configType]
	if !found {
		pfxlog.Logger().Debugf("no service config of type %v defined for service %v", configType, service.Name)
		return false, nil
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:     target,
		DecodeHook: mapstructure.StringToTimeDurationHookFunc(),
	})

	if err != nil {
		pfxlog.Logger().WithError(err).Debugf("unable to setup decoder for service configuration for type %v defined for service %v", configType, service.Name)
		return true, errors.Wrap(err, "unable to setup decoder for service config structure")
	}

	if err := decoder.Decode(configMap); err != nil {
		pfxlog.Logger().WithError(err).Debugf("unable to decode service configuration for type %v defined for service %v", configType, service.Name)
		return true, errors.Wrap(err, "unable to decode service config structure")
	}
	return true, nil
}

type apiResponse struct {
	Data interface{}          `json:"data"`
	Meta *ApiResponseMetadata `json:"meta"`
}

type ApiResponseMetadata struct {
	FilterableFields []string `json:"filterableFields"`
	Pagination       *struct {
		Offset     int `json:"offset"`
		Limit      int `json:"limit"`
		TotalCount int `json:"totalCount"`
	} `json:"pagination"`
}

func ApiResponseDecode(data interface{}, resp io.Reader) (*ApiResponseMetadata, error) {
	apiR := &apiResponse{
		Data: data,
	}

	if err := json.NewDecoder(resp).Decode(apiR); err != nil {
		return nil, err
	}

	return apiR.Meta, nil
}
