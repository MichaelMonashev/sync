package netmutex

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

)

func warn(a ...interface{}) {
	fmt.Fprintln(os.Stderr, a...)
}

func errorln(a ...interface{}) error {
	return errors.New(fmt.Sprintln(a...))
}

func nilCheck(a interface{}) {
	if a == nil {
		warn("value must not be nil!")
		debug.PrintStack()
		os.Exit(1)
	}
}
