package main

import (
	"fmt"
	"os"
)

func cmd_help() {
	fmt.Fprintln(os.Stderr, `proxyd - Pocket-sized proxy orchestration service

Commands:
  proxy    - start a proxy daemon
  rpc      - start an rpc server (testing)
  radius   - start a RADIUS server (testing)
  tokengen - generate a new RPC instance token
  help     - display this help message
  version  - display version
	`)
}
