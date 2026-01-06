// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logging_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gke-labs/multicluster-leader-election/pkg/logging"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap/zapcore"
)

type ExampleError struct{}

func (e *ExampleError) Error() string {
	return "ExampleError"
}

// TestBuildLogger tests that buildLogger sets up log entries to be in JSON,
// with fields compatible with GCP Cloud Logging, and that it respects the
// configured log level.
func TestBuildLogger(t *testing.T) {
	testCases := []struct {
		name    string
		level   zapcore.Level
		logFunc func(logger logr.Logger)
		wantLog bool
		want    map[string]interface{}
	}{
		{
			name:    "info level, log info",
			level:   zapcore.InfoLevel,
			logFunc: func(logger logr.Logger) { logger.Info("info message") },
			wantLog: true,
			want: map[string]interface{}{
				"msg":      "info message",
				"severity": "info",
			},
		},
		{
			name:    "info level, log debug",
			level:   zapcore.InfoLevel,
			logFunc: func(logger logr.Logger) { logger.V(1).Info("debug message") },
			wantLog: false,
		},
		{
			name:    "debug level, log info",
			level:   zapcore.DebugLevel,
			logFunc: func(logger logr.Logger) { logger.Info("info message") },
			wantLog: true,
			want: map[string]interface{}{
				"msg":      "info message",
				"severity": "info",
			},
		},
		{
			name:    "debug level, log debug",
			level:   zapcore.DebugLevel,
			logFunc: func(logger logr.Logger) { logger.V(1).Info("debug message") },
			wantLog: true,
			want: map[string]interface{}{
				"msg":      "debug message",
				"severity": "debug",
			},
		},
		{
			name:  "info level, log error",
			level: zapcore.InfoLevel,
			logFunc: func(logger logr.Logger) {
				logger.Error(&ExampleError{}, "error message")
			},
			wantLog: true,
			want: map[string]interface{}{
				"msg":      "error message",
				"severity": "error",
				"error":    "ExampleError",
			},
		},
		{
			name:  "debug level, log error",
			level: zapcore.DebugLevel,
			logFunc: func(logger logr.Logger) {
				logger.Error(&ExampleError{}, "error message")
			},
			wantLog: true,
			want: map[string]interface{}{
				"msg":      "error message",
				"severity": "error",
				"error":    "ExampleError",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			logger := logging.BuildLogger(&buffer, tc.level)
			tc.logFunc(logger)
			logOutput := buffer.String()

			if !tc.wantLog {
				if logOutput != "" {
					t.Errorf("expected no log output, but got: %s", logOutput)
				}
				return
			}

			if logOutput == "" {
				t.Fatal("expected log output, but got none")
			}

			var got map[string]interface{}
			if err := json.Unmarshal([]byte(logOutput), &got); err != nil {
				t.Fatalf("failed to unmarshal log output: %v", err)
			}

			// Check for timestamp and remove it for comparison
			if _, ok := got["timestamp"]; !ok {
				t.Fatal("log message does not contain `timestamp`")
			}
			delete(got, "timestamp")
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("unexpected log output (-want +got):\n%s", diff)
			}
		})
	}
}
