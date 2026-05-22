// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus_test

import (
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/stretchr/testify/assert"
)

func Test_SoakStartRequest_Validate(t *testing.T) {
	validReq := consensus.SoakStartRequest{
		NodeID:            "0.0.3",
		MigrationPlanPath: "/opt/solo/weaver/migration/consensus/0.0.3-20250521T143022Z-migration-plan.yaml",
		CutoverTimestamp:  time.Now(),
	}

	tests := []struct {
		name    string
		req     consensus.SoakStartRequest
		wantErr string
	}{
		{
			name: "valid",
			req:  validReq,
		},
		{
			name:    "missing node_id",
			req:     consensus.SoakStartRequest{MigrationPlanPath: validReq.MigrationPlanPath, CutoverTimestamp: validReq.CutoverTimestamp},
			wantErr: "node_id is required",
		},
		{
			name:    "missing migration_plan_path",
			req:     consensus.SoakStartRequest{NodeID: validReq.NodeID, CutoverTimestamp: validReq.CutoverTimestamp},
			wantErr: "migration_plan_path is required",
		},
		{
			name:    "missing cutover_timestamp",
			req:     consensus.SoakStartRequest{NodeID: validReq.NodeID, MigrationPlanPath: validReq.MigrationPlanPath},
			wantErr: "cutover_timestamp is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tc.wantErr)
			}
		})
	}
}
