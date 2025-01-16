// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
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

package profiler

import (
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"github.com/bonree-smartagent/profiling-go/profiler/internal"
	"github.com/cihub/seelog"
	"os"

	"gopkg.in/DataDog/dd-trace-go.v1/profiler"
)

var (
	isStarted  = false
	stoped     = false
	currentUrl string
)

func init() {
	logToConsole := os.Getenv("BONREE_GOAGENT_LOGTOCONSOLE")
	if logToConsole == "1" || logToConsole == "true" {
		l, err := seelog.LoggerFromWriterWithMinLevelAndFormat(os.Stdout,
			seelog.DebugLvl, "%Time [%LEVEL] %FuncShort @ %File.%Line %Msg%n")
		if err != nil {
			return
		}
		log.SetupLogger(l, seelog.DebugStr)
	}
}

// Start profiler
func Start() {
	enabled, err := internal.Init()
	if !enabled && err != nil {
		_ = log.Errorf("%v", err)
		return
	}

	go internal.RunLoop(func() bool {
		url := internal.GetUrl()
		client := internal.NewClient()
		if url == "" || client == nil {
			internalStop()
			return !stoped
		}

		if isStarted {
			if url == currentUrl {
				return !stoped
			}
		}

		currentUrl = url
		isStarted = true
		err = profiler.Start(
			profiler.WithAPIKey("00000000000000000000000000000000"),
			profiler.WithAgentlessUpload(),
			profiler.WithURL(url),
			profiler.WithHTTPClient(client),
			profiler.WithProfileTypes(
				profiler.CPUProfile,
				profiler.HeapProfile,
			),
		)

		if err != nil {
			isStarted = false
			_ = log.Errorf("%v", err)
		}
		return !stoped
	}, func() {
		internalStop()
	})
}

func internalStop() {
	isStarted = false
	currentUrl = ""
	profiler.Stop()
}

// Stop profiler
func Stop() {
	stoped = true
	internalStop()
}
