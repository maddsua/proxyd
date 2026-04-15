package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/maddsua/proxyd/utils"
)

func main() {

	exitCh := make(chan os.Signal, 1)
	signal.Notify(exitCh, os.Interrupt, syscall.SIGTERM)

	args := utils.NewArgList()

	switch cmd, _ := args.Next(); cmd {
	case "proxy", "":
		cmd_proxy(args, exitCh)
	case "rpc":
		cmd_rpc(args, exitCh)
	case "radius":
		cmd_radius(args, exitCh)
	case "tokengen":
		cmd_tokengen(args)
	default:
		fmt.Fprintln(os.Stderr, "Unexpected command:", cmd)
		fmt.Fprintln(os.Stderr, "Usage: proxyd <command> <args> <flags>")
		os.Exit(1)
	}
}
