// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package transaction

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/apm-server/model/metadata"
	"github.com/elastic/apm-server/transform"
	"github.com/elastic/beats/libbeat/common"
)

func TestTransactionEventDecodeFailure(t *testing.T) {
	for _, test := range []struct {
		input       interface{}
		err, inpErr error
		e           *Event
	}{
		{input: nil, err: errors.New("Input missing for decoding Event"), e: nil},
		{input: nil, inpErr: errors.New("a"), err: errors.New("a"), e: nil},
		{input: "", err: errors.New("Invalid type for transaction event"), e: nil},
		{input: map[string]interface{}{}, err: errors.New("Error fetching field"), e: nil},
	} {
		transformable, err := DecodeEvent(test.input, test.inpErr)
		assert.Equal(t, test.err, err)
		if test.e != nil {
			event := transformable.(*Event)
			assert.Equal(t, test.e, event)
		} else {
			assert.Nil(t, transformable)
		}
	}
}

func TestTransactionEventDecode(t *testing.T) {
	id, trType, name, result := "123", "type", "foo()", "555"
	timestamp := "2017-05-30T18:53:27.154Z"
	timestampParsed, _ := time.Parse(time.RFC3339, timestamp)
	timestampEpoch := json.Number(fmt.Sprintf("%d", timestampParsed.UnixNano()/1000))
	traceId, parentId := "0147258369012345abcdef0123456789", "abcdef0123456789"
	dropped, started, duration := 12, 6, 1.67
	context := map[string]interface{}{"a": "b"}
	marks := map[string]interface{}{"k": "b"}
	sampled := true

	for _, test := range []struct {
		input interface{}
		err   error
		e     *Event
	}{
		// traceId missing
		{
			input: map[string]interface{}{
				"id": id, "type": trType, "duration": duration, "timestamp": timestampEpoch,
				"span_count": map[string]interface{}{"started": 6.0}},
			err: errors.New("Error fetching field"),
		},
		// minimal event
		{
			input: map[string]interface{}{
				"id": id, "type": trType, "duration": duration, "timestamp": timestampEpoch,
				"trace_id": traceId, "span_count": map[string]interface{}{"started": 6.0}},
			e: &Event{
				Id: id, Type: trType, TraceId: traceId,
				Duration: duration, Timestamp: timestampParsed,
				SpanCount: SpanCount{Started: &started},
			},
		},
		// full event, ignoring spans
		{
			input: map[string]interface{}{
				"id": id, "type": trType, "name": name, "result": result,
				"duration": duration, "timestamp": timestampEpoch,
				"context": context, "marks": marks, "sampled": sampled,
				"parent_id": parentId, "trace_id": traceId,
				"spans": []interface{}{
					map[string]interface{}{
						"name": "span", "type": "db", "start": 1.2, "duration": 2.3,
					}},
				"span_count": map[string]interface{}{"dropped": 12.0, "started": 6.0}},
			e: &Event{Id: id, Type: trType, Name: &name, Result: &result,
				ParentId: &parentId, TraceId: traceId,
				Duration: duration, Timestamp: timestampParsed,
				Context: context, Marks: marks, Sampled: &sampled,
				SpanCount: SpanCount{Dropped: &dropped, Started: &started},
			},
		},
	} {
		transformable, err := DecodeEvent(test.input, nil)
		assert.Equal(t, test.err, err)
		if test.e != nil {
			event := transformable.(*Event)
			assert.Equal(t, test.e, event)
		}
	}
}

func TestEventTransform(t *testing.T) {

	id := "123"
	result := "tx result"
	sampled := false
	dropped, startedSpans := 5, 14
	name := "mytransaction"

	tests := []struct {
		Event  Event
		Output common.MapStr
		Msg    string
	}{
		{
			Event: Event{},
			Output: common.MapStr{
				"id":       "",
				"type":     "",
				"duration": common.MapStr{"us": 0},
				"sampled":  true,
			},
			Msg: "Empty Event",
		},
		{
			Event: Event{
				Id:       id,
				Type:     "tx",
				Duration: 65.98,
			},
			Output: common.MapStr{
				"id":       id,
				"type":     "tx",
				"duration": common.MapStr{"us": 65980},
				"sampled":  true,
			},
			Msg: "SpanCount empty",
		},
		{
			Event: Event{
				Id:        id,
				Type:      "tx",
				Duration:  65.98,
				SpanCount: SpanCount{Started: &startedSpans},
			},
			Output: common.MapStr{
				"id":         id,
				"type":       "tx",
				"duration":   common.MapStr{"us": 65980},
				"span_count": common.MapStr{"started": 14},
				"sampled":    true,
			},
			Msg: "SpanCount only contains `started`",
		},
		{
			Event: Event{
				Id:        id,
				Type:      "tx",
				Duration:  65.98,
				SpanCount: SpanCount{Dropped: &dropped},
			},
			Output: common.MapStr{
				"id":         id,
				"type":       "tx",
				"duration":   common.MapStr{"us": 65980},
				"span_count": common.MapStr{"dropped": common.MapStr{"total": 5}},
				"sampled":    true,
			},
			Msg: "SpanCount only contains `dropped`",
		},
		{
			Event: Event{
				Id:        id,
				Name:      &name,
				Type:      "tx",
				Result:    &result,
				Timestamp: time.Now(),
				Duration:  65.98,
				Context:   common.MapStr{"foo": "bar"},
				Sampled:   &sampled,
				SpanCount: SpanCount{Started: &startedSpans, Dropped: &dropped},
			},
			Output: common.MapStr{
				"id":         id,
				"name":       "mytransaction",
				"type":       "tx",
				"result":     "tx result",
				"duration":   common.MapStr{"us": 65980},
				"span_count": common.MapStr{"started": 14, "dropped": common.MapStr{"total": 5}},
				"sampled":    false,
			},
			Msg: "Full Event",
		},
	}

	tctx := &transform.Context{}

	for idx, test := range tests {
		output := test.Event.Transform(tctx)
		assert.Equal(t, test.Output, output[0].Fields["transaction"], fmt.Sprintf("Failed at idx %v; %s", idx, test.Msg))
	}
}

func TestEventsTransformWithMetadata(t *testing.T) {
	hostname := "a.b.c"
	architecture := "darwin"
	platform := "x64"
	timestamp, _ := time.Parse(time.RFC3339, "2019-01-03T15:17:04.908596+01:00")
	timestampUs := timestamp.UnixNano() / 1000

	service := metadata.Service{Name: "myservice"}
	system := &metadata.System{
		Hostname:     &hostname,
		Architecture: &architecture,
		Platform:     &platform,
	}

	txValid := Event{Timestamp: timestamp}
	txValidEs := common.MapStr{
		"agent": common.MapStr{"name": "", "version": ""},
		"processor": common.MapStr{
			"event": "transaction",
			"name":  "transaction",
		},
		"service": common.MapStr{
			"name": "myservice",
		},
		"transaction": common.MapStr{
			"duration": common.MapStr{"us": 0},
			"id":       "",
			"type":     "",
			"sampled":  true,
		},
		"timestamp": common.MapStr{"us": timestampUs},
	}

	txValidWithSystem := common.MapStr{
		"agent": common.MapStr{"name": "", "version": ""},
		"host": common.MapStr{
			"architecture": architecture,
			"hostname":     hostname,
			"os": common.MapStr{
				"platform": platform,
			},
		},
		"processor": common.MapStr{
			"event": "transaction",
			"name":  "transaction",
		},
		"service": common.MapStr{
			"name": "myservice",
		},
		"timestamp": common.MapStr{"us": timestampUs},
		"transaction": common.MapStr{
			"duration": common.MapStr{"us": 0},
			"id":       "",
			"type":     "",
			"sampled":  true,
		},
	}
	txWithContext := Event{Timestamp: timestamp, Context: common.MapStr{"foo": "bar", "user": common.MapStr{"id": "55"}}}
	txWithContextEs := common.MapStr{
		"agent": common.MapStr{"name": "", "version": ""},
		"context": common.MapStr{
			"foo":  "bar",
			"user": common.MapStr{"id": "55"},
		},
		"host": common.MapStr{
			"architecture": "darwin",
			"hostname":     "a.b.c",
			"os": common.MapStr{
				"platform": "x64",
			},
		},
		"processor": common.MapStr{
			"event": "transaction",
			"name":  "transaction",
		},
		"service": common.MapStr{
			"name": "myservice",
		},
		"timestamp": common.MapStr{"us": timestampUs},
		"transaction": common.MapStr{
			"duration": common.MapStr{"us": 0},
			"id":       "",
			"type":     "",
			"sampled":  true,
		},
	}

	txValidWithSpan := Event{Timestamp: timestamp}
	spanEs := common.MapStr{
		"agent": common.MapStr{"name": "", "version": ""},
		"processor": common.MapStr{
			"event": "span",
			"name":  "transaction",
		},
		"service": common.MapStr{
			"name": "myservice",
		},
		"span": common.MapStr{
			"duration": common.MapStr{"us": 0},
			"name":     "",
			"type":     "",
		},
		"timestamp": common.MapStr{"us": timestampUs},
	}

	tests := []struct {
		Metadata *metadata.Metadata
		Event    transform.Transformable
		Output   []common.MapStr
		Msg      string
	}{
		{
			Metadata: metadata.NewMetadata(
				&service,
				nil, nil, nil,
			),
			Event:  &txValid,
			Output: []common.MapStr{txValidEs},
			Msg:    "Payload with multiple Events",
		}, {
			Metadata: metadata.NewMetadata(
				&service,
				nil, nil, nil,
			),
			Event:  &txValidWithSpan,
			Output: []common.MapStr{txValidEs, spanEs},
			Msg:    "Payload with multiple Events",
		},

		{
			Metadata: metadata.NewMetadata(
				&service, system,
				nil, nil,
			),
			Event:  &txValid,
			Output: []common.MapStr{txValidWithSystem},
			Msg:    "Payload with System and Event",
		},
		{
			Metadata: metadata.NewMetadata(
				&service, system,
				nil, nil,
			),
			Event:  &txWithContext,
			Output: []common.MapStr{txWithContextEs},
			Msg:    "Payload with Service, System and Event with context",
		},
	}

	for idx, test := range tests {
		tctx := &transform.Context{
			Metadata:    *test.Metadata,
			RequestTime: timestamp,
		}
		outputEvents := test.Event.Transform(tctx)

		for j, outputEvent := range outputEvents {
			assert.Equal(t, test.Output[j], outputEvent.Fields, fmt.Sprintf("Failed at idx %v (j: %v); %s", idx, j, test.Msg))
			assert.Equal(t, timestamp, outputEvent.Timestamp, fmt.Sprintf("Failed at idx %v (j: %v); %s", idx, j, test.Msg))
		}
	}
}

func TestEventTransformUseReqTime(t *testing.T) {
	reqTimestamp := "2017-05-30T18:53:27.154Z"
	reqTimestampParsed, err := time.Parse(time.RFC3339, reqTimestamp)
	require.NoError(t, err)

	e := Event{}
	beatEvent := e.Transform(&transform.Context{RequestTime: reqTimestampParsed})
	require.Len(t, beatEvent, 1)
	assert.Equal(t, reqTimestampParsed, beatEvent[0].Timestamp)
}
