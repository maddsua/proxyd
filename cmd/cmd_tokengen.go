package main

import (
	"fmt"
	"os"

	"github.com/maddsua/proxyd/rpc"
	"github.com/maddsua/proxyd/utils"
)

func cmd_tokengen(args *utils.ArgList) {

	token, err := rpc.NewInstanceToken()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to generate token:", err.Error())
		os.Exit(1)
	}

	var flagFormat bool

	for {

		arg, ok := args.Next()
		if !ok {
			break
		}

		switch arg {
		case "-e", "--expand":
			flagFormat = true
		default:
			fmt.Fprintln(os.Stderr, "Unexpected argument:", arg)
			fmt.Fprintln(os.Stderr, "Usage: proxyd tokengen [-expand]")
			os.Exit(1)
		}
	}

	if flagFormat {
		fmt.Fprintln(os.Stderr, "ID:", token.ID.String())
		fmt.Fprintln(os.Stderr, "Secret:", token.SecretKey.String())
		fmt.Fprintf(os.Stderr, "\n")
	}

	fmt.Println(token.String())
}
