// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/google/uuid"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands"
	"github.com/hashgraph/solo-weaver/internal/doctor"
)

func main() {
	traceId := uuid.NewString()
	ctx := context.WithValue(context.Background(), "traceId", traceId)
	err := commands.Execute(ctx)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}
}
