/*
	Copyright NetFoundry, Inc.

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

package metrics

import (
	"github.com/openziti/foundation/event"
	"github.com/openziti/foundation/metrics/metrics_pb"
	"github.com/openziti/foundation/util/cowslice"
)

var EventHandlerRegistry = cowslice.NewCowSlice(make([]Handler, 0))

func getMetricsEventHandlers() []Handler {
	return EventHandlerRegistry.Value().([]Handler)
}

// HandlerType is used to define known handler types
type HandlerType string

const (
	HandlerTypeInfluxDB HandlerType = "influxdb"
	HandlerTypeJSONFile HandlerType = "jsonfile"
	HandlerTypeFile     HandlerType = "file"
)

// Handler represents a sink for metric events
type Handler interface {
	// AcceptMetrics is called when new metrics become available
	AcceptMetrics(message *metrics_pb.MetricsMessage)
}

type HandlerF func(message *metrics_pb.MetricsMessage)

func (self HandlerF) AcceptMetrics(message *metrics_pb.MetricsMessage) {
	self(message)
}

type NilHandler struct{}

func (NilHandler) AcceptMetrics(message *metrics_pb.MetricsMessage) {}

type eventWrapper struct {
	msg *metrics_pb.MetricsMessage
}

func (e *eventWrapper) Handle() {
	for _, handler := range getMetricsEventHandlers() {
		handler.AcceptMetrics(e.msg)
	}
}

func NewDispatchWrapper(deletage func(event event.Event)) Handler {
	return &eventDispatcherWrapper{delegate: deletage}
}

type eventDispatcherWrapper struct {
	delegate func(event event.Event)
}

func (dispatcherWrapper *eventDispatcherWrapper) AcceptMetrics(message *metrics_pb.MetricsMessage) {
	eventWrapper := &eventWrapper{msg: message}
	dispatcherWrapper.delegate(eventWrapper)
}

func Init(cfg *Config) {
	if cfg != nil && cfg.handlers != nil {
		for handler := range cfg.handlers {
			cowslice.Append(EventHandlerRegistry, handler)
		}
	}
}
