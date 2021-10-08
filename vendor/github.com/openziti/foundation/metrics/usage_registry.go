package metrics

import (
	"fmt"
	"github.com/openziti/foundation/metrics/metrics_pb"
	cmap "github.com/orcaman/concurrent-map"
	"reflect"
	"time"
)

// UsageRegistry extends registry to allow collecting usage metrics
type UsageRegistry interface {
	Registry
	IntervalCounter(name string, intervalSize time.Duration) IntervalCounter
	FlushToHandler(handler Handler)
	Flush()
	StartReporting(eventSink Handler, reportInterval time.Duration, msgQueueSize int)
}

func NewUsageRegistry(sourceId string, tags map[string]string, closeNotify <-chan struct{}) UsageRegistry {
	registry := &usageRegistryImpl{
		registryImpl: registryImpl{
			sourceId:  sourceId,
			tags:      tags,
			metricMap: cmap.New(),
		},
		intervalBucketChan: make(chan *bucketEvent, 16),
		closeNotify:        closeNotify,
		flushNotify:        make(chan struct{}, 1),
	}

	return registry
}

type bucketEvent struct {
	interval *metrics_pb.MetricsMessage_IntervalCounter
	name     string
}

type usageRegistryImpl struct {
	registryImpl
	intervalBucketChan chan *bucketEvent
	intervalBuckets    []*bucketEvent
	flushNotify        chan struct{}
	closeNotify        <-chan struct{}
}

func (self *usageRegistryImpl) StartReporting(eventSink Handler, reportInterval time.Duration, msgQueueSize int) {
	msgEvents := make(chan *metrics_pb.MetricsMessage, msgQueueSize)
	go self.run(reportInterval, msgEvents)
	go self.sendMsgs(eventSink, msgEvents)
}

// NewIntervalCounter creates an IntervalCounter
func (self *usageRegistryImpl) IntervalCounter(name string, intervalSize time.Duration) IntervalCounter {
	metric, present := self.metricMap.Get(name)
	if present {
		intervalCounter, ok := metric.(IntervalCounter)
		if !ok {
			panic(fmt.Errorf("metric '%v' already exists and is not an interval counter. It is a %v", name, reflect.TypeOf(metric).Name()))
		}
		return intervalCounter
	}

	disposeF := func() { self.dispose(name) }
	intervalCounter := newIntervalCounter(name, intervalSize, self, time.Minute, time.Second*80, disposeF, self.closeNotify)
	self.metricMap.Set(name, intervalCounter)
	return intervalCounter
}

func (self *usageRegistryImpl) Poll() *metrics_pb.MetricsMessage {
	base := self.registryImpl.Poll()
	if base == nil && self.intervalBuckets == nil {
		return nil
	}

	var builder *messageBuilder
	if base == nil {
		builder = newMessageBuilder(self.sourceId, self.tags)
	} else {
		builder = (*messageBuilder)(base)
	}

	builder.addIntervalBucketEvents(self.intervalBuckets)
	self.intervalBuckets = nil

	return (*metrics_pb.MetricsMessage)(builder)
}

func (self *usageRegistryImpl) reportInterval(counter *intervalCounterImpl, intervalStartUTC int64, values map[string]uint64) {
	bucket := &metrics_pb.MetricsMessage_IntervalBucket{
		IntervalStartUTC: intervalStartUTC,
		Values:           values,
	}

	interval := &metrics_pb.MetricsMessage_IntervalCounter{
		IntervalLength: uint64(counter.intervalSize.Seconds()),
		Buckets:        []*metrics_pb.MetricsMessage_IntervalBucket{bucket},
	}

	bucketEvent := &bucketEvent{
		interval: interval,
		name:     counter.name,
	}

	self.intervalBucketChan <- bucketEvent
}

func (self *usageRegistryImpl) run(reportInterval time.Duration, msgEvents chan *metrics_pb.MetricsMessage) {
	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()

	for {
		select {
		case interval := <-self.intervalBucketChan:
			self.intervalBuckets = append(self.intervalBuckets, interval)
		case <-ticker.C:
			if msg := self.Poll(); msg != nil {
				msgEvents <- msg
			}
		case <-self.flushNotify:
			if msg := self.Poll(); msg != nil {
				msgEvents <- msg
			}
		case <-self.closeNotify:
			self.DisposeAll()
			return
		}
	}
}

func (self *usageRegistryImpl) sendMsgs(eventSink Handler, msgEvents chan *metrics_pb.MetricsMessage) {
	for {
		select {
		case msg := <-msgEvents:
			eventSink.AcceptMetrics(msg)
		case <-self.closeNotify:
			return
		}
	}
}

func (self *usageRegistryImpl) FlushToHandler(handler Handler) {
	self.EachMetric(func(name string, metric Metric) {
		if ic, ok := metric.(*intervalCounterImpl); ok {
			ic.flush()
		}
	})
	time.Sleep(250 * time.Millisecond)
done:
	for {
		select {
		case interval := <-self.intervalBucketChan:
			self.intervalBuckets = append(self.intervalBuckets, interval)
		default:
			break done
		}
	}
	if msg := self.Poll(); msg != nil {
		handler.AcceptMetrics(msg)
	}
}

func (self *usageRegistryImpl) Flush() {
	self.EachMetric(func(name string, metric Metric) {
		if ic, ok := metric.(*intervalCounterImpl); ok {
			ic.flush()
		}
	})
	time.Sleep(250 * time.Millisecond)

	select {
	case self.flushNotify <- struct{}{}:
	default:
	}
}
