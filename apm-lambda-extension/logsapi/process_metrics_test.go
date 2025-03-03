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

package logsapi

import (
	"context"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"elastic/apm-lambda-extension/extension"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_processPlatformReportColdstart(t *testing.T) {

	mc := extension.MetadataContainer{
		Metadata: []byte(fmt.Sprintf(`{"metadata":{"service":{"agent":{"name":"apm-lambda-extension","version":"%s"},"framework":{"name":"AWS Lambda","version":""},"language":{"name":"python","version":"3.9.8"},"runtime":{"name":"","version":""},"node":{}},"user":{},"process":{"pid":0},"system":{"container":{"id":""},"kubernetes":{"node":{},"pod":{}}},"cloud":{"provider":"","instance":{},"machine":{},"account":{},"project":{},"service":{}}}}`, extension.Version)),
	}

	timestamp := time.Now()

	pm := PlatformMetrics{
		DurationMs:       182.43,
		BilledDurationMs: 183,
		MemorySizeMB:     128,
		MaxMemoryUsedMB:  76,
		InitDurationMs:   422.97,
	}

	logEventRecord := LogEventRecord{
		RequestId: "6f7f0961f83442118a7af6fe80b88d56",
		Status:    "Available",
		Metrics:   pm,
	}

	logEvent := LogEvent{
		Time:         timestamp,
		Type:         "platform.report",
		StringRecord: "",
		Record:       logEventRecord,
	}

	event := extension.NextEventResponse{
		Timestamp:          timestamp,
		EventType:          extension.Invoke,
		DeadlineMs:         timestamp.UnixNano()/1e6 + 4584, // Milliseconds
		RequestID:          "8476a536-e9f4-11e8-9739-2dfe598c3fcd",
		InvokedFunctionArn: "arn:aws:lambda:us-east-2:123456789012:function:custom-runtime",
		Tracing: extension.Tracing{
			Type:  "None",
			Value: "None",
		},
	}

	desiredOutputMetadata := fmt.Sprintf(`{"metadata":{"service":{"agent":{"name":"apm-lambda-extension","version":"%s"},"framework":{"name":"AWS Lambda","version":""},"language":{"name":"python","version":"3.9.8"},"runtime":{"name":"","version":""},"node":{}},"user":{},"process":{"pid":0},"system":{"container":{"id":""},"kubernetes":{"node":{},"pod":{}}},"cloud":{"provider":"","instance":{},"machine":{},"account":{},"project":{},"service":{}}}}`, extension.Version)

	desiredOutputMetrics := fmt.Sprintf(`{"metricset":{"samples":{"aws.lambda.metrics.coldstart_duration":{"value":422.9700012207031},"aws.lambda.metrics.timeout":{"value":5000},"system.memory.total":{"value":1.34217728e+08},"system.memory.actual.free":{"value":5.4525952e+07},"aws.lambda.metrics.duration":{"value":182.42999267578125},"aws.lambda.metrics.billed_duration":{"value":183}},"timestamp":%d,"faas":{"coldstart":true,"execution":"6f7f0961f83442118a7af6fe80b88d56","id":"arn:aws:lambda:us-east-2:123456789012:function:custom-runtime"}}}`, timestamp.UnixNano()/1e3)

	rawBytes, err := ProcessPlatformReport(context.Background(), &mc, &event, logEvent)
	require.NoError(t, err)

	requestBytes, err := extension.GetUncompressedBytes(rawBytes.Data, "")
	require.NoError(t, err)

	out := string(requestBytes)
	log.Println(out)

	processingResult := strings.Split(string(requestBytes), "\n")

	assert.JSONEq(t, desiredOutputMetadata, processingResult[0])
	assert.JSONEq(t, desiredOutputMetrics, processingResult[1])
}

func Test_processPlatformReportNoColdstart(t *testing.T) {

	mc := extension.MetadataContainer{
		Metadata: []byte(fmt.Sprintf(`{"metadata":{"service":{"agent":{"name":"apm-lambda-extension","version":"%s"},"framework":{"name":"AWS Lambda","version":""},"language":{"name":"python","version":"3.9.8"},"runtime":{"name":"","version":""},"node":{}},"user":{},"process":{"pid":0},"system":{"container":{"id":""},"kubernetes":{"node":{},"pod":{}}},"cloud":{"provider":"","instance":{},"machine":{},"account":{},"project":{},"service":{}}}}`, extension.Version)),
	}

	timestamp := time.Now()

	pm := PlatformMetrics{
		DurationMs:       182.43,
		BilledDurationMs: 183,
		MemorySizeMB:     128,
		MaxMemoryUsedMB:  76,
		InitDurationMs:   0,
	}

	logEventRecord := LogEventRecord{
		RequestId: "6f7f0961f83442118a7af6fe80b88d56",
		Status:    "Available",
		Metrics:   pm,
	}

	logEvent := LogEvent{
		Time:         timestamp,
		Type:         "platform.report",
		StringRecord: "",
		Record:       logEventRecord,
	}

	event := extension.NextEventResponse{
		Timestamp:          timestamp,
		EventType:          extension.Invoke,
		DeadlineMs:         timestamp.UnixNano()/1e6 + 4584, // Milliseconds
		RequestID:          "8476a536-e9f4-11e8-9739-2dfe598c3fcd",
		InvokedFunctionArn: "arn:aws:lambda:us-east-2:123456789012:function:custom-runtime",
		Tracing: extension.Tracing{
			Type:  "None",
			Value: "None",
		},
	}

	desiredOutputMetadata := fmt.Sprintf(`{"metadata":{"service":{"agent":{"name":"apm-lambda-extension","version":"%s"},"framework":{"name":"AWS Lambda","version":""},"language":{"name":"python","version":"3.9.8"},"runtime":{"name":"","version":""},"node":{}},"user":{},"process":{"pid":0},"system":{"container":{"id":""},"kubernetes":{"node":{},"pod":{}}},"cloud":{"provider":"","instance":{},"machine":{},"account":{},"project":{},"service":{}}}}`, extension.Version)

	desiredOutputMetrics := fmt.Sprintf(`{"metricset":{"samples":{"aws.lambda.metrics.coldstart_duration":{"value":0},"aws.lambda.metrics.timeout":{"value":5000},"system.memory.total":{"value":1.34217728e+08},"system.memory.actual.free":{"value":5.4525952e+07},"aws.lambda.metrics.duration":{"value":182.42999267578125},"aws.lambda.metrics.billed_duration":{"value":183}},"timestamp":%d,"faas":{"coldstart":false,"execution":"6f7f0961f83442118a7af6fe80b88d56","id":"arn:aws:lambda:us-east-2:123456789012:function:custom-runtime"}}}`, timestamp.UnixNano()/1e3)

	rawBytes, err := ProcessPlatformReport(context.Background(), &mc, &event, logEvent)
	require.NoError(t, err)

	requestBytes, err := extension.GetUncompressedBytes(rawBytes.Data, "")
	require.NoError(t, err)

	out := string(requestBytes)
	log.Println(out)

	processingResult := strings.Split(string(requestBytes), "\n")

	assert.JSONEq(t, desiredOutputMetadata, processingResult[0])
	assert.JSONEq(t, desiredOutputMetrics, processingResult[1])
}
