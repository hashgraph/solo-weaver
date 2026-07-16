// SPDX-License-Identifier: Apache-2.0

// Command statuszmock runs the mock Block Node statusz REST server for daemon
// development and the UTM-VM traffic-shaper demo. It serves
// `statusz/inbound-clients` and `statusz/outbound-clients` from a JSON roster
// file that is re-read on every request, so editing the file changes what the
// daemon's poll loop sees on its next tick.
//
// Usage:
//
//	go run ./internal/daemon/blocknode/statuszmock/cmd --addr :8080 --roster roster.json
//
// The roster JSON shape is:
//
//	{
//	  "inbound":  [ { "remote": {"address":"10.10.1.0/24","port":"*"}, "category":"publisher" } ],
//	  "outbound": [ { "remote": {"address":"10.30.5.7","port":"43473"}, "category":"peer_bn" } ]
//	}
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/hashgraph/solo-weaver/internal/daemon/blocknode/statuszmock"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	roster := flag.String("roster", "roster.json", "path to the JSON roster file (re-read on every request)")
	flag.Parse()

	handler := statuszmock.Handler(statuszmock.FileRoster(*roster))

	log.Printf("mock statusz server listening on %s, serving roster %s", *addr, *roster)
	log.Printf("  GET %s/statusz/inbound-clients", *addr)
	log.Printf("  GET %s/statusz/outbound-clients", *addr)

	srv := &http.Server{Addr: *addr, Handler: handler}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("mock statusz server stopped: %v", err)
	}
}
