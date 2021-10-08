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

package channel2

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	defaultOutstandingConnects = 16
	defaultQueuedConnects      = 1
	defaultConnectTimeoutMs    = 1000

	minQueuedConnects      = 1
	minOutstandingConnects = 1
	minConnectTimeoutMs    = 30

	maxQueuedConnects      = 5000
	maxOutstandingConnects = 1000
	maxConnectTimeoutMs    = 60000
)

type Options struct {
	OutQueueSize int
	BindHandlers []BindHandler
	PeekHandlers []PeekHandler
	ConnectOptions
	DelayRxStart bool
}

func DefaultOptions() *Options {
	return &Options{
		OutQueueSize:   4,
		ConnectOptions: DefaultConnectOptions(),
	}
}

func DefaultConnectOptions() ConnectOptions {
	return ConnectOptions{
		MaxQueuedConnects:      defaultQueuedConnects,
		MaxOutstandingConnects: defaultOutstandingConnects,
		ConnectTimeoutMs:       defaultConnectTimeoutMs,
	}
}

func LoadOptions(data map[interface{}]interface{}) *Options {
	options := DefaultOptions()

	if value, found := data["outQueueSize"]; found {
		if floatValue, ok := value.(float64); ok {
			options.OutQueueSize = int(floatValue)
		}
	}

	if value, found := data["maxQueuedConnects"]; found {
		if intVal, ok := value.(int); ok {
			options.MaxQueuedConnects = intVal
		}
	}

	if value, found := data["maxOutstandingConnects"]; found {
		if intVal, ok := value.(int); ok {
			options.MaxOutstandingConnects = intVal
		}
	}

	if value, found := data["connectTimeoutMs"]; found {
		if intVal, ok := value.(int); ok {
			options.ConnectTimeoutMs = intVal
		}
	}

	return options
}

func (o Options) String() string {
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(data)
}

type ConnectOptions struct {
	MaxQueuedConnects      int
	MaxOutstandingConnects int
	ConnectTimeoutMs       int
}

func (co *ConnectOptions) Validate() error {
	if err := co.validateConnectTimeout(); err != nil {
		return err
	}

	if err := co.validateOutstandingConnects(); err != nil {
		return err
	}

	if err := co.validateQueueConnects(); err != nil {
		return err
	}

	return nil
}

func (co *ConnectOptions) validateQueueConnects() error {
	if co.MaxQueuedConnects < minQueuedConnects {
		return fmt.Errorf("maxQueuedConnects must be at least %d", minQueuedConnects)
	} else if co.MaxQueuedConnects > maxQueuedConnects {
		return fmt.Errorf("maxQueuedConnects must be at most %d", maxQueuedConnects)
	}
	return nil
}

func (co *ConnectOptions) validateOutstandingConnects() error {
	if co.MaxOutstandingConnects < minOutstandingConnects {
		return fmt.Errorf("maxOutstandingConnects must be at least %d", minOutstandingConnects)
	} else if co.MaxOutstandingConnects > maxOutstandingConnects {
		return fmt.Errorf("maxOutstandingConnects must be at most %d", maxOutstandingConnects)
	}

	return nil
}

func (co *ConnectOptions) validateConnectTimeout() error {
	if co.ConnectTimeoutMs < minConnectTimeoutMs {
		return fmt.Errorf("connectTimeoutMs must be at least %d ms", minConnectTimeoutMs)
	} else if co.ConnectTimeoutMs > maxConnectTimeoutMs {
		return fmt.Errorf("connectTimeoutMs must be at most %d ms", maxConnectTimeoutMs)
	}

	return nil
}

func (co *ConnectOptions) ConnectTimeout() time.Duration {
	return time.Duration(co.ConnectTimeoutMs) * time.Millisecond
}
