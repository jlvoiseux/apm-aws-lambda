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

package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"elastic/apm-lambda-extension/extension"
	"elastic/apm-lambda-extension/logsapi"
)

var (
	extensionName   = filepath.Base(os.Args[0]) // extension name has to match the filename
	extensionClient = extension.NewClient(os.Getenv("AWS_LAMBDA_RUNTIME_API"))
)

/* --- elastic vars  --- */

func main() {

	// Global context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-sigs
		cancel()
		extension.Log.Infof("Received %v\n, exiting", s)
	}()

	// pulls ELASTIC_ env variable into globals for easy access
	config := extension.ProcessEnv()
	extension.Log.Level.SetLevel(config.LogLevel)

	// register extension with AWS Extension API
	res, err := extensionClient.Register(ctx, extensionName)
	if err != nil {
		status, errRuntime := extensionClient.InitError(ctx, err.Error())
		if errRuntime != nil {
			panic(errRuntime)
		}
		extension.Log.Errorf("Error: %s", err)
		extension.Log.Infof("Init error signal sent to runtime : %s", status)
		extension.Log.Infof("Exiting")
		return
	}
	extension.Log.Debugf("Register response: %v", extension.PrettyPrint(res))

	// Init APM Server Transport struct and start http server to receive data from agent
	apmServerTransport := extension.InitApmServerTransport(config)
	agentDataServer, err := extension.StartHttpServer(ctx, apmServerTransport)
	if err != nil {
		extension.Log.Errorf("Could not start APM data receiver : %v", err)
	}
	defer agentDataServer.Close()

	// Use a wait group to ensure the background go routine sending to the APM server
	// completes before signaling that the extension is ready for the next invocation.

	logsTransport, err := logsapi.Subscribe(ctx, extensionClient.ExtensionID, []logsapi.EventType{logsapi.Platform})
	if err != nil {
		extension.Log.Warnf("Error while subscribing to the Logs API: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var backgroundDataSendWg sync.WaitGroup
			processEvent(ctx, cancel, apmServerTransport, logsTransport, &backgroundDataSendWg)
			extension.Log.Debug("Waiting for background data send to end")
			backgroundDataSendWg.Wait()
			if config.SendStrategy == extension.SyncFlush {
				// Flush APM data now that the function invocation has completed
				apmServerTransport.FlushAPMData(ctx)
			}
		}
	}
}

func processEvent(ctx context.Context, cancel context.CancelFunc, apmServerTransport *extension.ApmServerTransport, logsTransport *logsapi.LogsTransport, backgroundDataSendWg *sync.WaitGroup) {
	// Invocation context
	invocationCtx, invocationCancel := context.WithCancel(ctx)
	defer invocationCancel()

	// call Next method of extension API.  This long polling HTTP method
	// will block until there's an invocation of the function
	extension.Log.Infof("Waiting for next event...")
	event, err := extensionClient.NextEvent(ctx)
	if err != nil {
		status, err := extensionClient.ExitError(ctx, err.Error())
		if err != nil {
			panic(err)
		}
		extension.Log.Errorf("Error: %s", err)
		extension.Log.Infof("Exit signal sent to runtime : %s", status)
		extension.Log.Infof("Exiting")
		return
	}

	extension.Log.Debug("Received event.")
	extension.Log.Debugf("%v", extension.PrettyPrint(event))

	if event.EventType == extension.Shutdown {
		cancel()
		return
	}

	// APM Data Processing
	backgroundDataSendWg.Add(1)
	go func() {
		defer backgroundDataSendWg.Done()
		if err := apmServerTransport.ForwardApmData(invocationCtx); err != nil {
			extension.Log.Error(err)
		}
	}()

	// Lambda Service Logs Processing
	runtimeDone := make(chan struct{})
	go func() {
		if err := logsapi.WaitRuntimeDone(invocationCtx, event.RequestID, logsTransport, runtimeDone); err != nil {
			extension.Log.Errorf("Error while processing Lambda Logs ; %v", err)
		} else {
			close(runtimeDone)
		}
	}()

	// Calculate how long to wait for a runtimeDoneSignal or AgentDoneSignal signal
	flushDeadlineMs := event.DeadlineMs - 100
	durationUntilFlushDeadline := time.Until(time.Unix(flushDeadlineMs/1000, 0))

	// Create a timer that expires after durationUntilFlushDeadline
	timer := time.NewTimer(durationUntilFlushDeadline)
	defer timer.Stop()

	select {
	case <-apmServerTransport.AgentDoneSignal:
		extension.Log.Debug("Received agent done signal")
	case <-runtimeDone:
		extension.Log.Debug("Received runtimeDone signal")
	case <-timer.C:
		extension.Log.Info("Time expired waiting for agent signal or runtimeDone event")
	}
}
