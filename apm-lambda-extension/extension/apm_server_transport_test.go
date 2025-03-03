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

package extension

import (
	"compress/gzip"
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPostToApmServerDataCompressed(t *testing.T) {

	s := "A long time ago in a galaxy far, far away..."

	// Compress the data
	pr, pw := io.Pipe()
	gw, _ := gzip.NewWriterLevel(pw, gzip.BestSpeed)
	go func() {
		if _, err := gw.Write([]byte(s)); err != nil {
			t.Fail()
			return
		}
		if err := gw.Close(); err != nil {
			t.Fail()
			return
		}
		if err := pw.Close(); err != nil {
			t.Fail()
			return
		}
	}()

	// Create AgentData struct with compressed data
	data, _ := ioutil.ReadAll(pr)
	agentData := AgentData{Data: data, ContentEncoding: "gzip"}

	// Create apm server and handler
	apmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes, _ := ioutil.ReadAll(r.Body)
		assert.Equal(t, string(data), string(bytes))
		assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		if _, err := w.Write([]byte(`{"foo": "bar"}`)); err != nil {
			t.Fail()
			return
		}
	}))
	defer apmServer.Close()

	config := extensionConfig{
		apmServerUrl: apmServer.URL + "/",
	}
	transport := InitApmServerTransport(&config)
	err := transport.PostToApmServer(context.Background(), agentData)
	assert.Equal(t, nil, err)
}

func TestPostToApmServerDataNotCompressed(t *testing.T) {
	s := "A long time ago in a galaxy far, far away..."
	body := []byte(s)
	agentData := AgentData{Data: body, ContentEncoding: ""}

	// Compress the data, so it can be compared with what
	// the apm server receives
	pr, pw := io.Pipe()
	gw, _ := gzip.NewWriterLevel(pw, gzip.BestSpeed)
	go func() {
		if _, err := gw.Write([]byte(s)); err != nil {
			t.Fail()
			return
		}
		if err := gw.Close(); err != nil {
			t.Fail()
			return
		}
		if err := pw.Close(); err != nil {
			t.Fail()
			return
		}
	}()

	// Create apm server and handler
	apmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBytes, _ := ioutil.ReadAll(r.Body)
		compressedBytes, _ := ioutil.ReadAll(pr)
		assert.Equal(t, string(compressedBytes), string(requestBytes))
		assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		if _, err := w.Write([]byte(`{"foo": "bar"}`)); err != nil {
			t.Fail()
			return
		}
	}))
	defer apmServer.Close()

	config := extensionConfig{
		apmServerUrl: apmServer.URL + "/",
	}
	transport := InitApmServerTransport(&config)
	err := transport.PostToApmServer(context.Background(), agentData)
	assert.Equal(t, nil, err)
}

func TestGracePeriod(t *testing.T) {
	transport := InitApmServerTransport(&extensionConfig{})

	transport.reconnectionCount = 0
	val0 := transport.computeGracePeriod().Seconds()
	assert.Equal(t, val0, float64(0))

	transport.reconnectionCount = 1
	val1 := transport.computeGracePeriod().Seconds()
	assert.InDelta(t, val1, float64(1), 0.1*1)

	transport.reconnectionCount = 2
	val2 := transport.computeGracePeriod().Seconds()
	assert.InDelta(t, val2, float64(4), 0.1*4)

	transport.reconnectionCount = 3
	val3 := transport.computeGracePeriod().Seconds()
	assert.InDelta(t, val3, float64(9), 0.1*9)

	transport.reconnectionCount = 4
	val4 := transport.computeGracePeriod().Seconds()
	assert.InDelta(t, val4, float64(16), 0.1*16)

	transport.reconnectionCount = 5
	val5 := transport.computeGracePeriod().Seconds()
	assert.InDelta(t, val5, float64(25), 0.1*25)

	transport.reconnectionCount = 6
	val6 := transport.computeGracePeriod().Seconds()
	assert.InDelta(t, val6, float64(36), 0.1*36)

	transport.reconnectionCount = 7
	val7 := transport.computeGracePeriod().Seconds()
	assert.InDelta(t, val7, float64(36), 0.1*36)
}

func TestSetHealthyTransport(t *testing.T) {
	transport := InitApmServerTransport(&extensionConfig{})
	transport.SetApmServerTransportState(context.Background(), Healthy)
	assert.True(t, transport.status == Healthy)
	assert.Equal(t, transport.reconnectionCount, -1)
}

func TestSetFailingTransport(t *testing.T) {
	// By explicitly setting the reconnection count to 0, we ensure that the grace period will not be 0
	// and avoid a race between reaching the pending status and the test assertion.
	transport := InitApmServerTransport(&extensionConfig{})
	transport.reconnectionCount = 0
	transport.SetApmServerTransportState(context.Background(), Failing)
	assert.True(t, transport.status == Failing)
	assert.Equal(t, transport.reconnectionCount, 1)
}

func TestSetPendingTransport(t *testing.T) {
	transport := InitApmServerTransport(&extensionConfig{})
	transport.SetApmServerTransportState(context.Background(), Healthy)
	transport.SetApmServerTransportState(context.Background(), Failing)
	for {
		if transport.status != Failing {
			break
		}
	}
	assert.True(t, transport.status == Pending)
	assert.Equal(t, transport.reconnectionCount, 0)
}

func TestSetPendingTransportExplicitly(t *testing.T) {
	transport := InitApmServerTransport(&extensionConfig{})
	transport.SetApmServerTransportState(context.Background(), Healthy)
	transport.SetApmServerTransportState(context.Background(), Pending)
	assert.True(t, transport.status == Healthy)
	assert.Equal(t, transport.reconnectionCount, -1)
}

func TestSetInvalidTransport(t *testing.T) {
	transport := InitApmServerTransport(&extensionConfig{})
	transport.SetApmServerTransportState(context.Background(), Healthy)
	transport.SetApmServerTransportState(context.Background(), "Invalid")
	assert.True(t, transport.status == Healthy)
	assert.Equal(t, transport.reconnectionCount, -1)
}

func TestEnterBackoffFromHealthy(t *testing.T) {
	// Compress the data
	pr, pw := io.Pipe()
	gw, _ := gzip.NewWriterLevel(pw, gzip.BestSpeed)
	go func() {
		if _, err := gw.Write([]byte("")); err != nil {
			t.Fail()
			return
		}
		if err := gw.Close(); err != nil {
			t.Fail()
			return
		}
		if err := pw.Close(); err != nil {
			t.Fail()
			return
		}
	}()

	// Create AgentData struct with compressed data
	data, _ := ioutil.ReadAll(pr)
	agentData := AgentData{Data: data, ContentEncoding: "gzip"}

	// Create apm server and handler
	apmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes, _ := ioutil.ReadAll(r.Body)
		assert.Equal(t, string(data), string(bytes))
		assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		if _, err := w.Write([]byte(`{"foo": "bar"}`)); err != nil {
			return
		}
	}))
	config := extensionConfig{
		apmServerUrl: apmServer.URL + "/",
	}
	transport := InitApmServerTransport(&config)
	transport.SetApmServerTransportState(context.Background(), Healthy)

	// Close the APM server early so that POST requests fail and that backoff is enabled
	apmServer.Close()

	if err := transport.PostToApmServer(context.Background(), agentData); err != nil {
		return
	}
	// No way to know for sure if failing or pending (0 sec grace period)
	assert.True(t, transport.status != Healthy)
	assert.Equal(t, transport.reconnectionCount, 0)
}

func TestEnterBackoffFromFailing(t *testing.T) {
	// Compress the data
	pr, pw := io.Pipe()
	gw, _ := gzip.NewWriterLevel(pw, gzip.BestSpeed)
	go func() {
		if _, err := gw.Write([]byte("")); err != nil {
			t.Fail()
			return
		}
		if err := gw.Close(); err != nil {
			t.Fail()
			return
		}
		if err := pw.Close(); err != nil {
			t.Fail()
			return
		}
	}()

	// Create AgentData struct with compressed data
	data, _ := ioutil.ReadAll(pr)
	agentData := AgentData{Data: data, ContentEncoding: "gzip"}

	// Create apm server and handler
	apmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes, _ := ioutil.ReadAll(r.Body)
		assert.Equal(t, string(data), string(bytes))
		assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		if _, err := w.Write([]byte(`{"foo": "bar"}`)); err != nil {
			t.Fail()
			return
		}
	}))
	// Close the APM server early so that POST requests fail and that backoff is enabled
	apmServer.Close()

	config := extensionConfig{
		apmServerUrl: apmServer.URL + "/",
	}

	transport := InitApmServerTransport(&config)
	transport.SetApmServerTransportState(context.Background(), Healthy)
	transport.SetApmServerTransportState(context.Background(), Failing)
	for {
		if transport.status != Failing {
			break
		}
	}
	assert.Equal(t, transport.status, Pending)

	assert.Error(t, transport.PostToApmServer(context.Background(), agentData))
	assert.Equal(t, transport.status, Failing)
	assert.Equal(t, transport.reconnectionCount, 1)
}

func TestAPMServerRecovery(t *testing.T) {
	// Compress the data
	pr, pw := io.Pipe()
	gw, _ := gzip.NewWriterLevel(pw, gzip.BestSpeed)
	go func() {
		if _, err := gw.Write([]byte("")); err != nil {
			t.Fail()
			return
		}
		if err := gw.Close(); err != nil {
			t.Fail()
			return
		}
		if err := pw.Close(); err != nil {
			t.Fail()
			return
		}
	}()

	// Create AgentData struct with compressed data
	data, _ := ioutil.ReadAll(pr)
	agentData := AgentData{Data: data, ContentEncoding: "gzip"}

	// Create apm server and handler
	apmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes, _ := ioutil.ReadAll(r.Body)
		assert.Equal(t, string(data), string(bytes))
		assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		if _, err := w.Write([]byte(`{"foo": "bar"}`)); err != nil {
			return
		}
	}))
	defer apmServer.Close()

	config := extensionConfig{
		apmServerUrl: apmServer.URL + "/",
	}

	transport := InitApmServerTransport(&config)
	transport.SetApmServerTransportState(context.Background(), Healthy)
	transport.SetApmServerTransportState(context.Background(), Failing)
	for {
		if transport.status != Failing {
			break
		}
	}
	assert.Equal(t, transport.status, Pending)

	assert.NoError(t, transport.PostToApmServer(context.Background(), agentData))
	assert.Equal(t, transport.status, Healthy)
	assert.Equal(t, transport.reconnectionCount, -1)
}

func TestContinuedAPMServerFailure(t *testing.T) {
	// Compress the data
	pr, pw := io.Pipe()
	gw, _ := gzip.NewWriterLevel(pw, gzip.BestSpeed)
	go func() {
		if _, err := gw.Write([]byte("")); err != nil {
			t.Fail()
			return
		}
		if err := gw.Close(); err != nil {
			t.Fail()
			return
		}
		if err := pw.Close(); err != nil {
			t.Fail()
			return
		}
	}()

	// Create AgentData struct with compressed data
	data, _ := ioutil.ReadAll(pr)
	agentData := AgentData{Data: data, ContentEncoding: "gzip"}

	// Create apm server and handler
	apmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes, _ := ioutil.ReadAll(r.Body)
		assert.Equal(t, string(data), string(bytes))
		assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		if _, err := w.Write([]byte(`{"foo": "bar"}`)); err != nil {
			return
		}
	}))
	apmServer.Close()

	config := extensionConfig{
		apmServerUrl: apmServer.URL + "/",
	}

	transport := InitApmServerTransport(&config)
	transport.SetApmServerTransportState(context.Background(), Healthy)
	transport.SetApmServerTransportState(context.Background(), Failing)
	for {
		if transport.status != Failing {
			break
		}
	}
	assert.Equal(t, transport.status, Pending)
	assert.Error(t, transport.PostToApmServer(context.Background(), agentData))
	assert.Equal(t, transport.status, Failing)
	assert.Equal(t, transport.reconnectionCount, 1)
}

func BenchmarkPostToAPM(b *testing.B) {

	// Create apm server and handler
	apmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(ioutil.Discard, r.Body); err != nil {
			return
		}
		if err := r.Body.Close(); err != nil {
			return
		}
		w.WriteHeader(202)
		if _, err := w.Write([]byte(`{}`)); err != nil {
			return
		}
	}))
	config := extensionConfig{
		apmServerUrl: apmServer.URL + "/",
	}

	transport := InitApmServerTransport(&config)

	// Copied from https://github.com/elastic/apm-server/blob/master/testdata/intake-v2/transactions.ndjson.
	benchBody := []byte(`{"metadata": {"service": {"name": "1234_service-12a3","node": {"configured_name": "node-123"},"version": "5.1.3","environment": "staging","language": {"name": "ecmascript","version": "8"},"runtime": {"name": "node","version": "8.0.0"},"framework": {"name": "Express","version": "1.2.3"},"agent": {"name": "elastic-node","version": "3.14.0"}},"user": {"id": "123user", "username": "bar", "email": "bar@user.com"}, "labels": {"tag0": null, "tag1": "one", "tag2": 2}, "process": {"pid": 1234,"ppid": 6789,"title": "node","argv": ["node","server.js"]},"system": {"hostname": "prod1.example.com","architecture": "x64","platform": "darwin", "container": {"id": "container-id"}, "kubernetes": {"namespace": "namespace1", "pod": {"uid": "pod-uid", "name": "pod-name"}, "node": {"name": "node-name"}}},"cloud":{"account":{"id":"account_id","name":"account_name"},"availability_zone":"cloud_availability_zone","instance":{"id":"instance_id","name":"instance_name"},"machine":{"type":"machine_type"},"project":{"id":"project_id","name":"project_name"},"provider":"cloud_provider","region":"cloud_region","service":{"name":"lambda"}}}}
{"transaction": { "id": "945254c567a5417e", "trace_id": "0123456789abcdef0123456789abcdef", "parent_id": "abcdefabcdef01234567", "type": "request", "duration": 32.592981,  "span_count": { "started": 43 }}}
{"transaction": {"id": "4340a8e0df1906ecbfa9", "trace_id": "0acd456789abcdef0123456789abcdef", "name": "GET /api/types","type": "request","duration": 32.592981,"outcome":"success", "result": "success", "timestamp": 1496170407154000, "sampled": true, "span_count": {"started": 17},"context": {"service": {"runtime": {"version": "7.0"}},"page":{"referer":"http://localhost:8000/test/e2e/","url":"http://localhost:8000/test/e2e/general-usecase/"}, "request": {"socket": {"remote_address": "12.53.12.1","encrypted": true},"http_version": "1.1","method": "POST","url": {"protocol": "https:","full": "https://www.example.com/p/a/t/h?query=string#hash","hostname": "www.example.com","port": "8080","pathname": "/p/a/t/h","search": "?query=string","hash": "#hash","raw": "/p/a/t/h?query=string#hash"},"headers": {"user-agent":["Mozilla/5.0 (Macintosh; Intel Mac OS X 10_10_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/51.0.2704.103 Safari/537.36","Mozilla Chrome Edge"],"content-type": "text/html","cookie": "c1=v1, c2=v2","some-other-header": "foo","array": ["foo","bar","baz"]},"cookies": {"c1": "v1","c2": "v2"},"env": {"SERVER_SOFTWARE": "nginx","GATEWAY_INTERFACE": "CGI/1.1"},"body": {"str": "hello world","additional": { "foo": {},"bar": 123,"req": "additional information"}}},"response": {"status_code": 200,"headers": {"content-type": "application/json"},"headers_sent": true,"finished": true,"transfer_size":25.8,"encoded_body_size":26.90,"decoded_body_size":29.90}, "user": {"domain": "ldap://abc","id": "99","username": "foo"},"tags": {"organization_uuid": "9f0e9d64-c185-4d21-a6f4-4673ed561ec8", "tag2": 12, "tag3": 12.45, "tag4": false, "tag5": null },"custom": {"my_key": 1,"some_other_value": "foo bar","and_objects": {"foo": ["bar","baz"]},"(": "not a valid regex and that is fine"}}}}
{"transaction": { "id": "cdef4340a8e0df19", "trace_id": "0acd456789abcdef0123456789abcdef", "type": "request", "duration": 13.980558, "timestamp": 1532976822281000, "sampled": null, "span_count": { "dropped": 55, "started": 436 }, "marks": {"navigationTiming": {"appBeforeBootstrap": 608.9300000000001,"navigationStart": -21},"another_mark": {"some_long": 10,"some_float": 10.0}, "performance": {}}, "context": { "request": { "socket": { "remote_address": "192.0.1", "encrypted": null }, "method": "POST", "headers": { "user-agent": null, "content-type": null, "cookie": null }, "url": { "protocol": null, "full": null, "hostname": null, "port": null, "pathname": null, "search": null, "hash": null, "raw": null } }, "response": { "headers": { "content-type": null } }, "service": {"environment":"testing","name": "service1","node": {"configured_name": "node-ABC"}, "language": {"version": "2.5", "name": "ruby"}, "agent": {"version": "2.2", "name": "elastic-ruby", "ephemeral_id": "justanid"}, "framework": {"version": "5.0", "name": "Rails"}, "version": "2", "runtime": {"version": "2.5", "name": "cruby"}}},"experience":{"cls":1,"fid":2.0,"tbt":3.4,"longtask":{"count":3,"sum":2.5,"max":1}}}}
{"transaction": { "id": "00xxxxFFaaaa1234", "trace_id": "0123456789abcdef0123456789abcdef", "name": "amqp receive", "parent_id": "abcdefabcdef01234567", "type": "messaging", "duration": 3, "span_count": { "started": 1 }, "context": {"message": {"queue": { "name": "new_users"}, "age":{ "ms": 1577958057123}, "headers": {"user_id": "1ax3", "involved_services": ["user", "auth"]}, "body": "user created", "routing_key": "user-created-transaction"}},"session":{"id":"sunday","sequence":123}}}
{"transaction": { "name": "july-2021-delete-after-july-31", "type": "lambda", "result": "success", "id": "142e61450efb8574", "trace_id": "eb56529a1f461c5e7e2f66ecb075e983", "subtype": null, "action": null, "duration": 38.853, "timestamp": 1631736666365048, "sampled": true, "context": { "cloud": { "origin": { "account": { "id": "abc123" }, "provider": "aws", "region": "us-east-1", "service": { "name": "serviceName" } } }, "service": { "origin": { "id": "abc123", "name": "service-name", "version": "1.0" } }, "user": {}, "tags": {}, "custom": { } }, "sync": true, "span_count": { "started": 0 }, "outcome": "unknown", "faas": { "coldstart": false, "execution": "2e13b309-23e1-417f-8bf7-074fc96bc683", "trigger": { "request_id": "FuH2Cir_vHcEMUA=", "type": "http" } }, "sample_rate": 1 } }
`)
	agentData := AgentData{Data: benchBody, ContentEncoding: ""}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := transport.PostToApmServer(context.Background(), agentData); err != nil {
			b.Fatal(err)
		}
	}
}
