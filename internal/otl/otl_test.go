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
	"github.com/stretchr/testify/suite"
	"testing"
)

type NotelTestSuite struct {
	suite.Suite
}

// SetupSuite sets up the whole suite
func (nts *NotelTestSuite) SetupSuite() {
}

func (nts *NotelTestSuite) TearDownSuite() {
}

func TestNotelTestSuite(t *testing.T) {
	suite.Run(t, new(NotelTestSuite))
}

func testNMTConfigWithOTelTLS() *OTel {
	config := &OTel{
		Enable: true,
		Collector: OTelCollectorConfig{
			Endpoint:      "0.0.0.0:4317",
			RetryInterval: "",
			TLS: OTelTLSConfig{
				Insecure:   false,
				CaFile:     "../../dev/scripts/gen/test-ca-cert.pem",
				CertFile:   "../../dev/scripts/gen/test-client-cert.pem",
				KeyFile:    "../../dev/scripts/gen/test-client-key.pem",
				MinVersion: "1.1",
				MaxVersion: "1.2",
			},
		},
		Trace: OTelTraceConfig{},
	}

	return config
}

func (nts *NotelTestSuite) TestParseTlsConfig() {
	req := nts.Require()
	otelConfig := testNMTConfigWithOTelTLS()
	ntl := New(WithOTelConfig(*otelConfig))
	tlsCred, err := ntl.parseTlsConfig()
	req.NoError(err)
	req.NotNil(tlsCred)
}

func (nts *NotelTestSuite) TestParseTlsConfigFailCaCert() {
	otelConfig := testNMTConfigWithOTelTLS()
	otelConfig.Collector.TLS.CaFile = "INVALID"
	ntl := New(WithOTelConfig(*otelConfig))

	tlsCred, err := ntl.parseTlsConfig()
	nts.Error(err)
	nts.Contains(err.Error(), "failed to load CA certificate")
	nts.Nil(tlsCred)
}

func (nts *NotelTestSuite) TestParseTlsConfigFailClientCert() {
	otelConfig := testNMTConfigWithOTelTLS()
	otelConfig.Collector.TLS.CertFile = "INVALID"
	ntl := New(WithOTelConfig(*otelConfig))

	tlsCred, err := ntl.parseTlsConfig()
	nts.Error(err)
	nts.Contains(err.Error(), "failed to load client certificate PEM")
	nts.Nil(tlsCred)

	otelConfig = testNMTConfigWithOTelTLS()
	otelConfig.Collector.TLS.KeyFile = "INVALID"
	ntl = New(WithOTelConfig(*otelConfig))

	tlsCred, err = ntl.parseTlsConfig()
	nts.Error(err)
	nts.Contains(err.Error(), "failed to load client certificate PEM")
	nts.Nil(tlsCred)
}
