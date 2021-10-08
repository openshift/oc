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
	"github.com/michaelquigley/pfxlog"
	"github.com/sirupsen/logrus"
	"reflect"
	"time"
)

// TODO: See if we can make interval processing use a single goroutine

// IntervalCounter allows tracking counters which are bucketized by some interval
type IntervalCounter interface {
	Metric
	Update(intervalId string, time time.Time, value uint64)
}

type intervalCounterReporter interface {
	reportInterval(counter *intervalCounterImpl, intervalStartUTC int64, values map[string]uint64)
}

func newIntervalCounter(name string,
	intervalSize time.Duration,
	reporter intervalCounterReporter,
	flushFreq time.Duration,
	ageThreshold time.Duration,
	disposeF func(),
	closeNotify <-chan struct{}) IntervalCounter {

	currentInterval := time.Now().Truncate(intervalSize).UTC().Unix()
	currentCounters := make(map[string]uint64)
	intervalMap := make(map[int64]map[string]uint64)
	intervalMap[currentInterval] = currentCounters

	var ticker *time.Ticker
	if flushFreq > 0 {
		ticker = time.NewTicker(flushFreq)
	}

	intervalCounter := &intervalCounterImpl{
		name:            name,
		intervalSize:    intervalSize,
		currentInterval: currentInterval,
		currentValues:   currentCounters,
		intervalMap:     intervalMap,
		eventChan:       make(chan interface{}, 10),
		reporter:        reporter,
		ticker:          ticker,
		ageThreshold:    ageThreshold,
		dispose:         disposeF,
		closeNotify:     closeNotify,
	}

	go intervalCounter.run()
	return intervalCounter
}

type counterEvent struct {
	intervalId string
	time       time.Time
	value      uint64
}

type intervalCounterImpl struct {
	name            string
	intervalSize    time.Duration
	currentInterval int64
	currentValues   map[string]uint64
	intervalMap     map[int64]map[string]uint64
	eventChan       chan interface{}
	reporter        intervalCounterReporter
	ticker          *time.Ticker
	ageThreshold    time.Duration
	dispose         func()
	closeNotify     <-chan struct{}
}

func (intervalCounter *intervalCounterImpl) Update(intervalId string, time time.Time, value uint64) {
	if value > 0 {
		event := &counterEvent{intervalId, time, value}

		// Select on this to make sure we don't block? If blocked, log to disk instead? Map updates should be
		// very fast, not sure that's needed
		intervalCounter.eventChan <- event
	}
}

func (intervalCounter *intervalCounterImpl) Dispose() {
	intervalCounter.dispose()
}

func (intervalCounter *intervalCounterImpl) report() {
	intervalCounter.eventChan <- time.Now()
}

func (intervalCounter *intervalCounterImpl) flush() {
	intervalCounter.eventChan <- time.Time{}
}

func (intervalCounter *intervalCounterImpl) run() {
	defer logrus.Debug("interval counter shutting down")

	var tickerC <-chan time.Time
	if intervalCounter.ticker != nil {
		tickerC = intervalCounter.ticker.C
	}

	for {
		select {
		case <-intervalCounter.closeNotify:
			if intervalCounter.ticker != nil {
				intervalCounter.ticker.Stop()
			}
			intervalCounter.currentValues = nil
			intervalCounter.intervalMap = nil
			return

		case <-tickerC:
			intervalCounter.flushIntervals()

		case i := <-intervalCounter.eventChan:
			switch event := i.(type) {
			case *counterEvent:
				interval := event.time.Truncate(intervalCounter.intervalSize).UTC().Unix()
				valueMap := intervalCounter.getValueMapForInterval(interval)
				valueMap[event.intervalId] += event.value
				break
			case time.Time:
				if event.IsZero() {
					intervalCounter.currentInterval++
				}
				intervalCounter.flushIntervals()
			default:
				pfxlog.Logger().Errorf("unhandled IntervalCounter event type '%v'", reflect.TypeOf(event).Name())
			}
		}
	}
}

func (intervalCounter *intervalCounterImpl) getValueMapForInterval(interval int64) map[string]uint64 {
	if intervalCounter.currentInterval == interval {
		return intervalCounter.currentValues
	}

	if result, found := intervalCounter.intervalMap[interval]; found {
		return result
	}

	result := make(map[string]uint64)
	intervalCounter.intervalMap[interval] = result

	if interval > intervalCounter.currentInterval {
		intervalCounter.currentInterval = interval
		intervalCounter.currentValues = result
	}

	return result
}

func (intervalCounter *intervalCounterImpl) flushIntervals() {
	threshold := time.Now().UTC().Add(-intervalCounter.ageThreshold).Unix()

	for interval, values := range intervalCounter.intervalMap {
		if interval < intervalCounter.currentInterval || interval <= threshold {
			if len(values) > 0 {
				intervalCounter.reporter.reportInterval(intervalCounter, interval, values)
				if interval == intervalCounter.currentInterval {
					intervalCounter.currentValues = make(map[string]uint64)
					intervalCounter.intervalMap[intervalCounter.currentInterval] = intervalCounter.currentValues
				} else {
					delete(intervalCounter.intervalMap, interval)
				}
			}
		}
	}
}
