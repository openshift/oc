package common

import (
	"fmt"
	"github.com/michaelquigley/pfxlog"
	"strconv"
	"strings"
)

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

type VersionProvider interface {
	Version() string
	BuildDate() string
	Revision() string
	Branch() string
	AsVersionInfo() *VersionInfo
	EncoderDecoder() VersionEncDec
}

type VersionEncDec interface {
	Encode(*VersionInfo) ([]byte, error)
	Decode([]byte) (*VersionInfo, error)
}

var developmentVersion = ParseSemVer("0.0.0")

func ParseSemVer(version string) *SemVer {
	result := &SemVer{}
	result.parse(version)
	return result
}

type SemVer struct {
	parts []uint
}

func (self *SemVer) parse(version string) {
	if strings.HasPrefix(version, "v") {
		version = strings.TrimPrefix(version, "v")
	}

	for _, part := range strings.Split(version, ".") {
		if self.parsePart(part) != nil {
			pfxlog.Logger().WithField("version", version).Info("unable to parse version")
			return
		}
	}
}

func (self *SemVer) parsePart(part string) error {
	val, err := strconv.ParseInt(part, 10, 32)
	if err == nil {
		self.parts = append(self.parts, uint(val))
	}
	return err
}

func (self *SemVer) CompareTo(version *SemVer) int {
	for idx, part := range self.parts {
		if len(version.parts) < idx+1 {
			return 1
		}
		if part > version.parts[idx] {
			return 1
		}
		if part < version.parts[idx] {
			return -1
		}
	}

	if len(version.parts) > len(self.parts) {
		return -1
	}

	return 0
}

func (self *SemVer) Equals(version *SemVer) bool {
	return self.CompareTo(version) == 0
}

func (self *SemVer) String() string {
	if len(self.parts) == 0 {
		return ""
	}
	result := strings.Builder{}
	result.WriteString(fmt.Sprintf("%v", self.parts[0]))

	for _, part := range self.parts[1:] {
		result.WriteString(fmt.Sprintf(".%v", part))
	}

	return result.String()
}

type VersionInfo struct {
	Version   string
	Revision  string
	BuildDate string
	OS        string
	Arch      string
}

func (self *VersionInfo) GetVersion() *SemVer {
	return ParseSemVer(self.Version)
}

func (self *VersionInfo) HasMinimumVersion(compareVersionStr string) bool {
	if self == nil {
		return false
	}
	compareVersion := ParseSemVer(compareVersionStr)
	version := self.GetVersion()
	return version.CompareTo(compareVersion) >= 0 || version.Equals(developmentVersion)
}

type VersionEncDecImpl struct{}

var StdVersionEncDec = VersionEncDecImpl{}

func (encDec *VersionEncDecImpl) Encode(info *VersionInfo) ([]byte, error) {
	out := fmt.Sprintf("%v|%v|%v|%v|%v", info.Version, info.Revision, info.BuildDate, info.OS, info.Arch)
	return []byte(out), nil
}

func (encDec *VersionEncDecImpl) Decode(info []byte) (*VersionInfo, error) {
	values := strings.Split(string(info), "|")

	if len(values) != 5 {
		return nil, fmt.Errorf("could not parse version info, expected 5 values got %d", len(values))
	}

	return &VersionInfo{
		Version:   values[0],
		Revision:  values[1],
		BuildDate: values[2],
		OS:        values[3],
		Arch:      values[4],
	}, nil

}
