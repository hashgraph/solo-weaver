// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/google/uuid"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands"
	"github.com/hashgraph/solo-weaver/internal/doctor"
)

// Entry point for the Weaver CLI application
//
// Commands structure:
// - weaver machine benchmark block | consensus | relay | proxy  --type cpu | memory | disk | network | all
// - weaver machine diag logs | config | all
//
// - weaver kube cluster install | uninstall | upgrade  | reset
// - weaver kube diag logs | config | all
//
// weaver block node install | uninstall | reset | upgrade | migrate
// weaver block node deploy | destroy
// weaver block diag logs/config/all
// weaver block network join | leave
//
// weaver consensus node start | stop | restart | install | uninstall
// weaver consensus diag logs/config/all
// weaver consensus network join | leave
func main() {
	traceId := uuid.NewString()
	ctx := context.WithValue(context.Background(), "traceId", traceId)
	err := commands.Execute(ctx)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}
}
