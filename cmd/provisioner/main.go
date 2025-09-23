package main

import (
	"context"
	"github.com/google/uuid"
	"golang.hedera.com/solo-provisioner/cmd/provisioner/commands"
	"golang.hedera.com/solo-provisioner/internal/doctor"
)

func main() {
	traceId := uuid.NewString()
	ctx := context.WithValue(context.Background(), "traceId", traceId)
	err := commands.Execute(ctx)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}
}
