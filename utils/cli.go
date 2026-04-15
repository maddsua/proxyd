package utils

import "os"

func NewArgList() *ArgList {
	return &ArgList{args: os.Args[1:]}
}

type ArgList struct {
	args []string
	idx  int
}

func (args *ArgList) HasNext() bool {
	return len(args.args) > 0 && args.idx < len(args.args)
}

func (args *ArgList) Next() (string, bool) {

	if !args.HasNext() {
		return "", false
	}

	val := args.args[args.idx]
	args.idx++

	return val, true
}

func (args *ArgList) NextOr(val string) string {
	next, ok := args.Next()
	if !ok {
		return val
	}
	return next
}
