package ziti

import (
	"github.com/openziti/sdk-golang/ziti/edge"
	"time"
)

type ServiceEventType string

const (
	ServiceAdded   ServiceEventType = "Added"
	ServiceRemoved ServiceEventType = "Removed"
	ServiceChanged ServiceEventType = "Changed"
)

type serviceCB func(eventType ServiceEventType, service *edge.Service)

type Options struct {
	RefreshInterval time.Duration
	OnContextReady  func(ctx Context)
	OnServiceUpdate serviceCB
}

var DefaultOptions = &Options{
	RefreshInterval: 5 * time.Minute,
	OnServiceUpdate: nil,
}
