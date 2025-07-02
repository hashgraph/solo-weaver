/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package otl

import (
	"bufio"
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
	"go.opentelemetry.io/otel/trace"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
	"testing"
)

type NotelLoggingTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel func()
	config OTel
	logger *zerolog.Logger
	span   trace.Span
}

// SetupSuite sets up the whole suite
func (s *NotelLoggingTestSuite) SetupSuite() {
	nl := zerolog.Nop()

	s.logger = &nl
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.config = OTel{
		Enable:    false,
		Collector: OTelCollectorConfig{},
		Trace:     OTelTraceConfig{},
	}

	Start(s.ctx,
		WithServiceName("notel-test"),
		WithOTelConfig(s.config),
		WithLogger(s.logger),
	)
}

func (s *NotelLoggingTestSuite) TearDownSuite() {
	if s.cancel != nil {
		s.cancel()
	}
	Shutdown()
}

// SetupTest sets up the test
func (s *NotelLoggingTestSuite) SetupTest() {
}

func (s *NotelLoggingTestSuite) TearDownTest() {
}

func (s *NotelLoggingTestSuite) BeforeTest(suiteName, testName string) {
}

func (s *NotelLoggingTestSuite) AfterTest(suitName, testName string) {
}

func (s *NotelLoggingTestSuite) TestDebug() {
	req := s.Require()

	tmpDir, err := os.MkdirTemp(os.TempDir(), "notel")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	tmpFile := fmt.Sprintf("%s/tmp.log", tmpDir)

	logger := logx.As()
	req.NoError(err)

	parentDebugMsg := ">>> Testing debug - PARENT SPAN <<<<"
	childDebugMsg := ">>> Testing debug - CHILD SPAN <<<<"

	ctxParent, parentSpan := StartParentSpan(logger)
	defer EndSpan(ctxParent, parentSpan)

	ctxChild, childSpan := StartSpan(ctxParent)
	defer EndSpan(ctxChild, childSpan)

	// check log messages
	f, err := os.Open(tmpFile)
	req.NoError(err)
	defer f.Close()
	reader := bufio.NewReader(f)

	line, _, err := reader.ReadLine()
	req.NoError(err)
	req.Contains(string(line), zerolog.DebugLevel.String())

	line, _, err = reader.ReadLine()
	req.NoError(err)
	req.Contains(string(line), parentDebugMsg)
	req.Contains(string(line), logFields.collectURL)

	line, _, err = reader.ReadLine()
	req.NoError(err)
	req.Contains(string(line), childDebugMsg)
	req.NotContains(string(line), logFields.collectURL)
}

func TestLoggingTestSuite(t *testing.T) {
	suite.Run(t, new(NotelLoggingTestSuite))
}
