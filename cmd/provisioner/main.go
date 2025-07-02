package main

import (
	"fmt"
	"golang.hedera.com/solo-provisioner/cmd/provisioner/commands"
	"os"
)

func main() {

	err := commands.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
