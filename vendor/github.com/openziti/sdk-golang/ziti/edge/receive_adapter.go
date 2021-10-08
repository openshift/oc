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
	"github.com/openziti/foundation/channel2"
)

type FunctionReceiveAdapter struct {
	Type    int32
	Handler func(*channel2.Message, channel2.Channel)
}

func (adapter *FunctionReceiveAdapter) ContentType() int32 {
	return adapter.Type
}

func (adapter *FunctionReceiveAdapter) HandleReceive(m *channel2.Message, ch channel2.Channel) {
	adapter.Handler(m, ch)
}

type AsyncFunctionReceiveAdapter struct {
	Type    int32
	Handler func(*channel2.Message, channel2.Channel)
}

func (adapter *AsyncFunctionReceiveAdapter) ContentType() int32 {
	return adapter.Type
}

func (adapter *AsyncFunctionReceiveAdapter) HandleReceive(m *channel2.Message, ch channel2.Channel) {
	go adapter.Handler(m, ch)
}
