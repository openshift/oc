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

package posture

import (
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/util/concurrenz"
	"github.com/openziti/foundation/util/stringz"
	"github.com/openziti/sdk-golang/ziti/edge"
	"github.com/openziti/sdk-golang/ziti/edge/api"
	cmap "github.com/orcaman/concurrent-map"
	"sync"
	"time"
)

type CacheData struct {
	Processes    cmap.ConcurrentMap // map[processPath]ProcessInfo
	MacAddresses []string
	Os           OsInfo
	Domain       string
	Evaluated    concurrenz.AtomicBoolean //marks whether posture responses for this data have been sent out
}

func NewCacheData() *CacheData {
	return &CacheData{
		Processes:    cmap.New(),
		MacAddresses: []string{},
		Os: OsInfo{
			Type:    "",
			Version: "",
		},
		Domain: "",
	}
}

type Cache struct {
	currentData  *CacheData
	previousData *CacheData

	watchedProcesses cmap.ConcurrentMap //map[processPath]struct{}{}

	serviceQueryMap map[string]map[string]edge.PostureQuery //map[serviceId]map[queryId]query
	activeServices  cmap.ConcurrentMap                      // map[serviceId]

	lastSent   cmap.ConcurrentMap //map[type|processQueryId]time.Time
	ctrlClient api.Client

	startOnce           sync.Once
	doSingleSubmissions bool
	closeNotify         <-chan struct{}

	DomainFunc func() string
}

func NewCache(ctrlClient api.Client, closeNotify <-chan struct{}) *Cache {
	cache := &Cache{
		currentData:      NewCacheData(),
		previousData:     NewCacheData(),
		watchedProcesses: cmap.New(),
		serviceQueryMap:  map[string]map[string]edge.PostureQuery{},
		activeServices:   cmap.New(),
		lastSent:         cmap.New(),
		ctrlClient:       ctrlClient,
		startOnce:        sync.Once{},
		closeNotify:      closeNotify,
		DomainFunc:       Domain,
	}
	cache.start()

	return cache
}

//Set the current list of processes paths that are being observed
func (cache *Cache) setWatchedProcesses(processPaths []string) {

	processMap := map[string]struct{}{}

	for _, processPath := range processPaths {
		processMap[processPath] = struct{}{}
		cache.watchedProcesses.Set(processPath, struct{}{})
	}

	var processesToRemove []string
	cache.watchedProcesses.IterCb(func(processPath string, _ interface{}) {
		if _, ok := processMap[processPath]; !ok {
			processesToRemove = append(processesToRemove, processPath)
		}
	})

	for _, processPath := range processesToRemove {
		cache.watchedProcesses.Remove(processPath)
	}
}

// Evaluate refreshes all posture data and determines if new posture responses should be sent out
func (cache *Cache) Evaluate() {
	cache.Refresh()
	if responses := cache.GetChangedResponses(); len(responses) > 0 {
		if err := cache.SendResponses(responses); err != nil {
			pfxlog.Logger().Error(err)
		}
	}
}

// GetChangedResponses determines if posture responses should be sent out.
func (cache *Cache) GetChangedResponses() []*api.PostureResponse {
	if !cache.currentData.Evaluated.CompareAndSwap(false, true) {
		return nil
	}

	activeQueryTypes := map[string]string{} // map[queryType|processPath]->queryId
	cache.activeServices.IterCb(func(serviceId string, _ interface{}) {
		queryMap, ok := cache.serviceQueryMap[serviceId]

		for queryId, query := range queryMap {
			if query.QueryType != api.PostureCheckTypeProcess {
				activeQueryTypes[query.QueryType] = queryId
			} else {
				activeQueryTypes[query.Process.Path] = queryId
			}
		}

		if !ok {
			return
		}
	})

	if len(activeQueryTypes) == 0 {
		return nil
	}

	var responses []*api.PostureResponse
	if cache.currentData.Domain != cache.previousData.Domain {
		if queryId, ok := activeQueryTypes[api.PostureCheckTypeDomain]; ok {
			domainRes := &api.PostureResponse{
				Id:     queryId,
				TypeId: api.PostureCheckTypeDomain,
				PostureSubType: api.PostureResponseDomain{
					Domain: cache.currentData.Domain,
				},
			}
			responses = append(responses, domainRes)
		}
	}

	if !stringz.EqualSlices(cache.currentData.MacAddresses, cache.previousData.MacAddresses) {
		if queryId, ok := activeQueryTypes[api.PostureCheckTypeMAC]; ok {
			macRes := &api.PostureResponse{
				Id:     queryId,
				TypeId: api.PostureCheckTypeMAC,
				PostureSubType: api.PostureResponseMac{
					MacAddresses: cache.currentData.MacAddresses,
				},
			}
			responses = append(responses, macRes)
		}
	}

	if cache.previousData.Os.Version != cache.currentData.Os.Version || cache.previousData.Os.Type != cache.previousData.Os.Type {
		if queryId, ok := activeQueryTypes[api.PostureCheckTypeOs]; ok {
			osRes := &api.PostureResponse{
				Id:     queryId,
				TypeId: api.PostureCheckTypeOs,
				PostureSubType: api.PostureResponseOs{
					Type:    cache.currentData.Os.Type,
					Version: cache.currentData.Os.Version,
					Build:   "",
				},
			}
			responses = append(responses, osRes)
		}
	}

	cache.currentData.Processes.IterCb(func(processPath string, processInfoVal interface{}) {
		curState, ok := processInfoVal.(ProcessInfo)
		if !ok {
			return
		}

		queryId, isActive := activeQueryTypes[processPath]

		if !isActive {
			return
		}

		prevVal, ok := cache.previousData.Processes.Get(processPath)

		sendResponse := false
		if !ok {
			//no prev state send
			sendResponse = true
		} else {
			prevState, ok := prevVal.(ProcessInfo)
			if !ok {
				sendResponse = true
			}

			sendResponse = prevState.IsRunning != curState.IsRunning || prevState.Hash != curState.Hash || !stringz.EqualSlices(prevState.SignerFingerprints, curState.SignerFingerprints)
		}

		if sendResponse {
			procResp := &api.PostureResponse{
				Id:     queryId,
				TypeId: api.PostureCheckTypeProcess,
				PostureSubType: api.PostureResponseProcess{
					IsRunning:          curState.IsRunning,
					Hash:               curState.Hash,
					SignerFingerprints: curState.SignerFingerprints,
				},
			}
			responses = append(responses, procResp)
		}
	})

	return responses
}

// Refresh refreshes posture data
func (cache *Cache) Refresh() {
	cache.previousData = cache.currentData

	cache.currentData = NewCacheData()
	cache.currentData.Os = Os()

	cache.currentData.Domain = cache.DomainFunc()
	cache.currentData.MacAddresses = MacAddresses()

	keys := cache.watchedProcesses.Keys()
	for _, processPath := range keys {
		cache.currentData.Processes.Set(processPath, Process(processPath))
	}
}

// SetServiceQueryMap receives of a list of serviceId -> queryId -> queries. Used to determine which queries are necessary
// to provide data for on a per service basis.
func (cache *Cache) SetServiceQueryMap(serviceQueryMap map[string]map[string]edge.PostureQuery) {
	cache.serviceQueryMap = serviceQueryMap

	var processPaths []string
	for _, queryMap := range serviceQueryMap {
		for _, query := range queryMap {
			if query.QueryType == api.PostureCheckTypeProcess && query.Process != nil {
				processPaths = append(processPaths, query.Process.Path)
			}
		}
	}
	cache.setWatchedProcesses(processPaths)
}

func (cache *Cache) AddActiveService(serviceId string) {
	cache.activeServices.Set(serviceId, struct{}{})
	cache.Evaluate()
}

func (cache *Cache) RemoveActiveService(serviceId string) {
	cache.activeServices.Remove(serviceId)
	cache.Evaluate()
}

func (cache *Cache) start() {
	cache.startOnce.Do(func() {
		ticker := time.NewTicker(10 * time.Second)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					pfxlog.Logger().Errorf("error during posture response streaming: %v", r)
				}
			}()

			for {
				select {
				case <-ticker.C:
					cache.Evaluate()
				case <-cache.closeNotify:
					return
				}
			}
		}()
	})
}

func (cache *Cache) SendResponses(responses []*api.PostureResponse) error {
	if cache.doSingleSubmissions {
		allErrors := api.Errors{}
		for _, response := range responses {
			err := cache.ctrlClient.SendPostureResponse(*response)

			if err != nil {
				allErrors.Errors = append(allErrors.Errors, err)
			}
		}

		if len(allErrors.Errors) != 0 {
			return allErrors
		}

		return nil

	} else {
		err := cache.ctrlClient.SendPostureResponseBulk(responses)

		if _, ok := err.(api.NotFound); ok {
			cache.doSingleSubmissions = true
			return cache.SendResponses(responses)
		}
		return err
	}
}
