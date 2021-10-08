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
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/michaelquigley/pfxlog"
	"github.com/pkg/errors"
	"io"
)

/**
 * Message headers notes
 * 0-127 reserved for channel
 * 128-255 reserved for headers that need to be reflected back to sender on responses
 *  128 is used for a message UUID for tracing
 * 1000-1099 reserved for edge messages
 * 1100-1199 is reserved for control plane messages
 * 2000-2500 is reserved for xgress messages
 *   2000-2255 is reserved for xgress implementation headers
 */
const (
	ConnectionIdHeader              = 0
	ReplyForHeader                  = 1
	ResultSuccessHeader             = 2
	HelloRouterAdvertisementsHeader = 3
	HelloVersionHeader              = 4

	// Headers in the range 128-255 inclusive will be reflected when creating replies
	ReflectedHeaderBitMask = 1 << 7
	MaxReflectedHeader     = (1 << 8) - 1
)

const magicLength = 4

type readFunction func(io.Reader) (*Message, error)
type marshalFunction func(m *Message) ([]byte, []byte, error)

type MessageHeader struct {
	ContentType int32
	sequence    int32
	replyFor    *int32
	Headers     map[int32][]byte
}

func (header *MessageHeader) Sequence() int32 {
	return header.sequence
}

func (header *MessageHeader) cacheReplyFor() {
	if header.replyFor == nil {
		replyFor, found := header.Headers[ReplyForHeader]
		if found {
			if len(replyFor) != 4 {
				pfxlog.Logger().Warnf("incorrect replyFor encoding. length should be 4 not %v", len(replyFor))
			} else {
				val := int32(binary.LittleEndian.Uint32(replyFor))
				header.replyFor = &val
			}
		}

		if replyFor == nil {
			val := int32(-1)
			header.replyFor = &val
		}
	}
}

func (header *MessageHeader) ReplyFor() int32 {
	header.cacheReplyFor()
	return *header.replyFor
}

func (header *MessageHeader) IsReply() bool {
	header.cacheReplyFor()
	return *header.replyFor != -1
}

func (header *MessageHeader) IsReplyingTo(sequence int32) bool {
	header.cacheReplyFor()
	return *header.replyFor == sequence
}

func (header *MessageHeader) PutUint64Header(key int32, value uint64) {
	encoded := make([]byte, 8)
	binary.LittleEndian.PutUint64(encoded, value)
	header.Headers[key] = encoded
}

func (header *MessageHeader) GetUint64Header(key int32) (uint64, bool) {
	encoded, ok := header.Headers[key]
	if !ok || len(encoded) != 8 {
		return 0, ok
	}
	result := binary.LittleEndian.Uint64(encoded)
	return result, true
}

func (header *MessageHeader) PutUint32Header(key int32, value uint32) {
	encoded := make([]byte, 4)
	binary.LittleEndian.PutUint32(encoded, value)
	header.Headers[key] = encoded
}

func (header *MessageHeader) GetUint32Header(key int32) (uint32, bool) {
	encoded, ok := header.Headers[key]
	if !ok || len(encoded) != 4 {
		return 0, false
	}
	result := binary.LittleEndian.Uint32(encoded)
	return result, true
}

func (header *MessageHeader) PutUint16Header(key int32, value uint16) {
	encoded := make([]byte, 2)
	binary.LittleEndian.PutUint16(encoded, value)
	header.Headers[key] = encoded
}

func (header *MessageHeader) GetUint16Header(key int32) (uint16, bool) {
	encoded, ok := header.Headers[key]
	if !ok || len(encoded) != 2 {
		return 0, false
	}
	result := binary.LittleEndian.Uint16(encoded)
	return result, true
}

func (header *MessageHeader) PutByteHeader(key int32, value byte) {
	header.Headers[key] = []byte{value}
}

func (header *MessageHeader) GetByteHeader(key int32) (byte, bool) {
	encoded, ok := header.Headers[key]
	if !ok || len(encoded) < 1 {
		return 0, ok
	}
	return encoded[0], true
}

func (header *MessageHeader) PutBoolHeader(key int32, value bool) {
	byteVal := byte(0)
	if value {
		byteVal = 1
	}
	header.Headers[key] = []byte{byteVal}
}

func (header *MessageHeader) GetBoolHeader(key int32) (bool, bool) {
	encoded, ok := header.Headers[key]
	if !ok {
		return false, ok
	}
	result := len(encoded) > 0 && encoded[0] == 1
	return result, true
}

func (header *MessageHeader) GetStringHeader(key int32) (string, bool) {
	encoded, ok := header.Headers[key]
	return string(encoded), ok
}

type Message struct {
	MessageHeader
	Body []byte
}

func NewMessage(contentType int32, body []byte) *Message {
	return &Message{
		MessageHeader: MessageHeader{
			ContentType: contentType,
			sequence:    -1,
			Headers:     make(map[int32][]byte),
		},
		Body: body,
	}
}

func (m *Message) ReplyTo(o *Message) {
	replyFor := o.sequence
	m.replyFor = &replyFor
	for key, value := range o.Headers {
		if key&ReflectedHeaderBitMask != 0 && key <= MaxReflectedHeader {
			m.Headers[key] = value
		}
	}
}

func (m *Message) String() string {
	if m.IsReply() {
		return fmt.Sprintf("//ct:[%4d]/sq:[%4d]/rf:[%4d]/l:[%4d]", m.ContentType, m.sequence, m.replyFor, len(m.Body))
	} else {
		return fmt.Sprintf("//ct:[%4d]/sq:[%4d]/rf:[    ]/l:[%4d]", m.ContentType, m.sequence, len(m.Body))
	}
}

var magicUnknownVersion = []byte{0x03, 0x06, 0x09, 0x0a}

const versionLen = 4

/*
 * Channel V2 Wire Format
 *
 *  [ message section ]
 * <marker:[]byte{0x03,0x06,0x09,0x0c}>				0  1  2  3
 * <content-type:int32>                             4  5  6  7
 * <sequence:int32>                                 8  9 10  11
 * <headers-length:int32>							12 13 14 15
 * <body-length:int32>								16 17 18 19
 *
 *  [ data section ]
 * <headers>										20 -> (20 + headers-length)
 * <body>											(20 + headers-length) -> (20 + headers-length + body-length)
 */
var magicV2 = []byte{0x03, 0x06, 0x09, 0x0c}

const dataSectionV2 = 20

var UnknownVersionError = errors.New("channel synchronization error, bad magic number")

var magicV3 = []byte{0x03, 0x06, 0x09, 0x0d}

type UnsupportedVersionError struct {
	supportedVersions []uint32
}

func (u UnsupportedVersionError) Error() string {
	return "server did not support requested channel version"
}

func readHello(peer io.Reader) (*Message, readFunction, marshalFunction, error) {
	version := make([]byte, versionLen)
	read, err := io.ReadFull(peer, version)

	defaultReadF := readV2
	defaultMarshalF := marshalV2

	if err != nil {
		return nil, defaultReadF, defaultMarshalF, err
	}

	if read != versionLen {
		return nil, defaultReadF, defaultMarshalF, errors.New("short read")
	}

	if bytes.Equal(version, magicV2) {
		msg, err := readHelloV2(peer)
		return msg, readV2, marshalV2, err
	}

	return nil, defaultReadF, defaultMarshalF, UnknownVersionError
}

func readHelloV2(peer io.Reader) (*Message, error) {
	messageSection := make([]byte, dataSectionV2)
	copy(messageSection, magicV2)
	read, err := io.ReadFull(peer, messageSection[versionLen:])
	if err != nil {
		return nil, err
	}
	if read != dataSectionV2-versionLen {
		return nil, errors.New("short read")
	}
	headersLength := readUint32(messageSection[12:16])
	bodyLength := readUint32(messageSection[16:20])
	if headersLength > 4192 || bodyLength > 4192 {
		return nil, fmt.Errorf("hello message too big. header len: %v, body len: %v", headersLength, bodyLength)
	}

	return unmarshalV2(peer, messageSection, headersLength, bodyLength)
}

func ReadWSMessage(peer io.Reader) (*Message, error) {
	return readV2(peer)
}

// readV2 reads a V2 message from the given reader and returns the unmarshalled message
func readV2(peer io.Reader) (*Message, error) {
	messageSection := make([]byte, dataSectionV2)
	read, err := io.ReadFull(peer, messageSection)
	if err != nil {
		return nil, err
	}

	if read < magicLength {
		return nil, errors.New("short read")
	}

	if !bytes.Equal(messageSection[0:magicLength], magicV2) {
		log := pfxlog.Logger()
		log.Debugf("received message version bytes: %v", messageSection[:4])
		if bytes.Equal(messageSection[0:magicLength], magicUnknownVersion) {
			log.Debug("message appears to be unknown version response")
			return nil, readUnknownVersionResponse(messageSection[4:], peer)
		}
		return nil, errors.New("channel synchronization")
	}

	headersLength := readUint32(messageSection[12:16])
	bodyLength := readUint32(messageSection[16:20])

	return unmarshalV2(peer, messageSection, headersLength, bodyLength)
}

// unmarshalV2 converts a block of V2 wire format data into a *Message.
func unmarshalV2(peer io.Reader, messageSectionData []byte, headersLength, bodyLength uint32) (*Message, error) {
	dataSectionData := make([]byte, headersLength+bodyLength)
	read, err := io.ReadFull(peer, dataSectionData)
	if err != nil {
		return nil, err
	}

	if read != int(headersLength+bodyLength) {
		return nil, errors.New("short read")
	}

	if len(messageSectionData) < dataSectionV2 {
		return nil, errors.New("short data stream")
	}

	if !bytes.Equal(messageSectionData[0:magicLength], magicV2) {
		return nil, errors.New("magic mismatch")
	}

	var headers map[int32][]byte
	if headersLength > 0 {
		headers, err = unmarshalHeaders(dataSectionData[:headersLength])
	} else {
		headers = make(map[int32][]byte)
	}
	if err != nil {
		return nil, err
	}
	m := &Message{
		MessageHeader: MessageHeader{
			ContentType: readInt32(messageSectionData[4:8]),
			sequence:    readInt32(messageSectionData[8:12]),
			Headers:     headers,
		},
		Body: dataSectionData[headersLength:],
	}
	return m, nil
}

/*
 * Channel V1 Headers Wire Format
 *
 * <key:int32> 			0  1  2  3
 * <length:int32>		4  5  6  7
 * <data>				8 -> (8 + length)
 */

func unmarshalHeaders(headerData []byte) (map[int32][]byte, error) {
	out := make(map[int32][]byte)
	if len(headerData) > 0 && len(headerData) < 8 {
		return nil, errors.New("truncated header data")
	}
	i := 0
	for i < len(headerData) {
		if (i + 8) > len(headerData) {
			return nil, fmt.Errorf("short header meta-data (%d >= %d)", i+8, len(headerData))
		}

		key := readInt32(headerData[i : i+4])
		length := readUint32(headerData[i+4 : i+8])
		if (i + 8 + int(length)) > len(headerData) {
			return nil, fmt.Errorf("short header data (%d >= %d)", i+8+int(length), len(headerData))
		}
		data := headerData[i+8 : i+8+int(length)]
		out[key] = data
		i += 8 + int(length)
	}
	return out, nil
}

// marshalV2 converts a *Message into a block of V2 wire format data.
func marshalV2(m *Message) ([]byte, []byte, error) {
	return marshalWithVersion(m, magicV2)
}

// marshalTest converts a *Message into a block of V3 wire format data.
// this is only here for testing, so we can test selection of an earlier
// supported version
func marshalV3(m *Message) ([]byte, []byte, error) {
	return marshalWithVersion(m, magicV3)
}

// marshalWithVersion converts a *Message into a block of V2 wire format data.
func marshalWithVersion(m *Message, version []byte) ([]byte, []byte, error) {
	data := new(bytes.Buffer)
	bodyData := new(bytes.Buffer)
	data.Write(version)
	if err := binary.Write(data, binary.LittleEndian, m.ContentType); err != nil { // content-type
		return nil, nil, err
	}
	if err := binary.Write(data, binary.LittleEndian, m.sequence); err != nil { // sequence
		return nil, nil, err
	}
	if m.replyFor != nil {
		replyForHeader := make([]byte, 4)
		binary.LittleEndian.PutUint32(replyForHeader, uint32(*m.replyFor))
		m.Headers[ReplyForHeader] = replyForHeader
	}
	headersData, err := marshalHeaders(m.Headers)
	if err != nil {
		return nil, nil, err
	}

	if err := binary.Write(data, binary.LittleEndian, int32(len(headersData))); err != nil { // header-length
		return nil, nil, err
	}
	if err := binary.Write(data, binary.LittleEndian, int32(len(m.Body))); err != nil { // body-length
		return nil, nil, err
	}
	n, err := bodyData.Write(headersData)
	if err != nil {
		return nil, nil, err
	}
	if n != len(headersData) {
		return nil, nil, errors.New("short headers write")
	}
	n, err = bodyData.Write(m.Body)
	if err != nil {
		return nil, nil, err
	}
	if n != len(m.Body) {
		return nil, nil, errors.New("short body write")
	}
	return data.Bytes(), bodyData.Bytes(), nil
}

func marshalHeaders(headers map[int32][]byte) ([]byte, error) {
	data := new(bytes.Buffer)
	for k, v := range headers {
		if err := binary.Write(data, binary.LittleEndian, k); err != nil {
			return nil, err
		}
		if err := binary.Write(data, binary.LittleEndian, int32(len(v))); err != nil {
			return nil, err
		}
		n, err := data.Write(v)
		if err != nil {
			return nil, err
		}
		if n != len(v) {
			return nil, errors.New("short header write")
		}
	}
	return data.Bytes(), nil
}

// readInt32 pulls a 4-byte int32 out of a byte array (or slice).
//
func readInt32(data []byte) int32 {
	return int32(binary.LittleEndian.Uint32(data))
}

func readUint32(data []byte) uint32 {
	return binary.LittleEndian.Uint32(data)
}

func writeUnknownVersionResponse(writer io.Writer) {
	data := new(bytes.Buffer)
	data.Write(magicUnknownVersion)

	for _, val := range []uint32{2, 1, 2} { // 2 versions being sent, version 1 and version 2
		if err := binary.Write(data, binary.LittleEndian, val); err != nil {
			pfxlog.Logger().WithError(err).Warnf("Unable to write value to bytes.Buffer")
			return
		}
	}

	written, err := writer.Write(data.Bytes())
	if err != nil {
		pfxlog.Logger().WithError(err).Warnf("Unable to write unknown message version response")
	} else if written != data.Len() {
		pfxlog.Logger().Warnf("Short write while writing unknown message version response")
	}
}

func readUnknownVersionResponse(initial []byte, reader io.Reader) error {
	log := pfxlog.Logger()
	if len(initial) < 4 {
		log.Debug("didn't receive enough bytes for an unknown version response")
		return errors.New("channel synchronization")
	}
	versionCount := binary.LittleEndian.Uint32(initial)
	buf := initial[4:]
	size := versionCount * 4

	if uint32(len(buf)) < size {
		leftover := buf
		buf := make([]byte, size)
		copy(buf, leftover)
		restBuf := buf[len(leftover):]
		if _, err := io.ReadFull(reader, restBuf); err != nil {
			log.Debugf("unable to read all %v bytes for unknown version response", len(restBuf))
			return errors.New("channel synchronization")
		}
	}

	var supported []uint32

	for len(buf) > 0 {
		version := binary.LittleEndian.Uint32(buf)
		supported = append(supported, version)
		buf = buf[4:]
	}

	return UnsupportedVersionError{supportedVersions: supported}
}

func getRetryVersion(err error) (uint32, bool) {
	return getRetryVersionFor(err, 2, 2)
}

func getRetryVersionFor(err error, defaultVersion uint32, localVersions ...uint32) (uint32, bool) {
	versionErr, ok := err.(UnsupportedVersionError)
	log := pfxlog.Logger()
	if ok && len(versionErr.supportedVersions) > 0 {
		log.Info("received unsupported version response from server")
		for _, version := range localVersions {
			for _, remoteVersion := range versionErr.supportedVersions {
				if remoteVersion == version {
					log.Infof("using highest supported version %v", version)
					return version, true
				}
			}
		}
	}

	log.Infof("defaulting to version %v", defaultVersion)
	return defaultVersion, false
}

type TypedMessage interface {
	proto.Message
	GetContentType() int32
}
