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
	"github.com/shirou/gopsutil/host"
	"regexp"
	"runtime"
	"strings"
)

type OsInfo struct {
	Type    string
	Version string
}

func Os() OsInfo {
	osType := "unknown"
	osVersion := "unknown"

	semVerParser := regexp.MustCompile(`^((\d+)\.(\d+)\.(\d+))`)

	_, family, version, _ := host.PlatformInformation()

	if runtime.GOOS == "windows" {
		if strings.EqualFold(family, "server") {
			osType = "windowsserver"
		} else {
			osType = "windows"
		}
	} else {
		osType = runtime.GOOS
	}

	parsedVersion := semVerParser.FindStringSubmatch(version)

	if len(parsedVersion) > 1 {
		osVersion = parsedVersion[0]
	}
	return OsInfo{
		Type:    osType,
		Version: osVersion,
	}
}
