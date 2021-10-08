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
	"fmt"
	"github.com/michaelquigley/pfxlog"
	"github.com/openziti/foundation/metrics/metrics_pb"
	"github.com/orcaman/concurrent-map"
	"github.com/pkg/errors"
	"github.com/rcrowley/go-metrics"
	"reflect"
)

// Metric is the base functionality for all metrics types
type Metric interface {
	Dispose()
}

// Registry allows for configuring and accessing metrics for a fabric application
type Registry interface {
	SourceId() string
	Gauge(name string) Gauge
	FuncGauge(name string, f func() int64) Gauge
	Meter(name string) Meter
	Histogram(name string) Histogram
	Timer(name string) Timer
	EachMetric(visitor func(name string, metric Metric))
	Poll() *metrics_pb.MetricsMessage
	DisposeAll()
}

func NewRegistry(sourceId string, tags map[string]string) Registry {
	return &registryImpl{
		sourceId:  sourceId,
		tags:      tags,
		metricMap: cmap.New(),
	}
}

type registryImpl struct {
	sourceId  string
	tags      map[string]string
	metricMap cmap.ConcurrentMap
}

func (registry *registryImpl) dispose(name string) {
	registry.metricMap.Remove(name)
}

func (registry *registryImpl) DisposeAll() {
	registry.EachMetric(func(name string, metric Metric) {
		metric.Dispose()
	})
}

func (registry *registryImpl) SourceId() string {
	return registry.sourceId
}

func (registry *registryImpl) Gauge(name string) Gauge {
	metric, present := registry.metricMap.Get(name)
	if present {
		gauge, ok := metric.(Gauge)
		if !ok {
			panic(fmt.Errorf("metric '%v' already exists and is not a gauge. It is a %v", name, reflect.TypeOf(metric).Name()))
		}
		return gauge
	}

	gauge := &gaugeImpl{
		Gauge: metrics.NewGauge(),
		dispose: func() {
			registry.dispose(name)
		},
	}
	registry.metricMap.Set(name, gauge)
	return gauge
}

func (registry *registryImpl) FuncGauge(name string, f func() int64) Gauge {
	metric, present := registry.metricMap.Get(name)
	if present {
		gauge, ok := metric.(Gauge)
		if !ok {
			panic(fmt.Errorf("metric '%v' already exists and is not a gauge. It is a %v", name, reflect.TypeOf(metric).Name()))
		}
		return gauge
	}

	gauge := &gaugeImpl{
		Gauge: metrics.NewFunctionalGauge(f),
		dispose: func() {
			registry.dispose(name)
		},
	}
	registry.metricMap.Set(name, gauge)
	return gauge
}

func (registry *registryImpl) Meter(name string) Meter {
	metric, present := registry.metricMap.Get(name)
	if present {
		meter, ok := metric.(Meter)
		if !ok {
			panic(fmt.Errorf("metric '%v' already exists and is not a meter. It is a %v", name, reflect.TypeOf(metric).Name()))
		}
		return meter
	}

	meter := &meterImpl{
		Meter: metrics.NewMeter(),
		dispose: func() {
			registry.dispose(name)
		},
	}
	registry.metricMap.Set(name, meter)
	return meter
}

func (registry *registryImpl) Histogram(name string) Histogram {
	metric, present := registry.metricMap.Get(name)
	if present {
		histogram, ok := metric.(Histogram)
		if !ok {
			panic(fmt.Errorf("metric '%v' already exists and is not a histogram. It is a %v", name, reflect.TypeOf(metric).Name()))
		}
		return histogram
	}

	histogram := &histogramImpl{
		Histogram: metrics.NewHistogram(metrics.NewExpDecaySample(128, 0.015)),
		dispose: func() {
			registry.dispose(name)
		},
	}
	registry.metricMap.Set(name, histogram)
	return histogram
}

func (registry *registryImpl) Timer(name string) Timer {
	metric, present := registry.metricMap.Get(name)
	if present {
		timer, ok := metric.(Timer)
		if !ok {
			panic(fmt.Errorf("metric '%v' already exists and is not a timer. It is a %v", name, reflect.TypeOf(metric).Name()))
		}
		return timer
	}

	timer := &timerImpl{
		Timer: metrics.NewTimer(),
		dispose: func() {
			registry.dispose(name)
		},
	}
	registry.metricMap.Set(name, timer)
	return timer
}

func (registry *registryImpl) EachMetric(visitor func(name string, metric Metric)) {
	for entry := range registry.metricMap.IterBuffered() {
		visitor(entry.Key, entry.Val.(Metric))
	}
}

func (registry *registryImpl) Each(visitor func(string, interface{})) {
	for entry := range registry.metricMap.IterBuffered() {
		visitor(entry.Key, entry.Val)
	}
}

// Provide rest of go-metrics Registry interface, so we can use go-metrics reporters if desired
func (registry *registryImpl) Get(s string) interface{} {
	val, _ := registry.metricMap.Get(s)
	return val
}

func (registry *registryImpl) GetAll() map[string]map[string]interface{} {
	return nil
}

func (registry *registryImpl) GetOrRegister(s string, i interface{}) interface{} {
	return registry.metricMap.Upsert(s, i, func(exist bool, valueInMap interface{}, newValue interface{}) interface{} {
		if exist {
			return valueInMap
		}
		return newValue
	})
}

func (registry *registryImpl) Register(s string, i interface{}) error {
	if registry.metricMap.SetIfAbsent(s, i) {
		return errors.Errorf("duplicate metric %v", s)
	}
	return nil
}

func (registry *registryImpl) RunHealthchecks() {
}

func (registry *registryImpl) Unregister(s string) {
	registry.metricMap.Remove(s)
}

func (registry *registryImpl) UnregisterAll() {
	for _, key := range registry.metricMap.Keys() {
		registry.Unregister(key)
	}
}

func (registry *registryImpl) Poll() *metrics_pb.MetricsMessage {
	// If there's nothing to report, skip it
	if registry.metricMap.Count() == 0 {
		return nil
	}

	builder := newMessageBuilder(registry.sourceId, registry.tags)

	registry.EachMetric(func(name string, i Metric) {
		switch metric := i.(type) {
		case *gaugeImpl:
			builder.addIntGauge(name, metric.Snapshot())
		case *meterImpl:
			builder.addMeter(name, metric.Snapshot())
		case *histogramImpl:
			builder.addHistogram(name, metric.Snapshot())
		case *timerImpl:
			builder.addTimer(name, metric.Snapshot())
		case *intervalCounterImpl:
			// ignore, handled below
		default:
			pfxlog.Logger().Errorf("Unsupported metric type %v", reflect.TypeOf(i))
		}
	})

	return (*metrics_pb.MetricsMessage)(builder)
}
