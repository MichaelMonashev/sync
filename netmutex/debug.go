package netmutex

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/MichaelMonashev/sync/netmutex/code"
)

func (c command) String() string {
	switch c.code {
	case code.LOCK:
		return fmt.Sprintf("LOCK %v %v %v", c.key, c.id, c.ttl)
	case code.UPDATE:
		return fmt.Sprintf("UPDATE %v %v %v %v", c.key, c.id, c.lockID, c.ttl)
	case code.UNLOCK:
		return fmt.Sprintf("UNLOCK %v %v %v", c.key, c.id, c.lockID)
	case code.UNLOCKALL:
		return fmt.Sprintf("UNLOCKALL %v", c.id)
	case code.PING:
		return fmt.Sprintf("PING %v", c.id)
	case code.CONNECT:
		return "CONNECT"
	default:
		return fmt.Sprintf("UNKNOWN command: %v", fmt.Sprint(c.code))
	}
}

func (r response) String() string {
	switch r.code {
	case code.OK:
		return fmt.Sprintf("OK %v", r.id)
	case code.REDIRECT:
		return fmt.Sprintf("REDIRECT %v to %v", r.id, r.serverID)
	case code.LOCKED:
		return fmt.Sprintf("LOCKED %v", r.id)
	case code.ERROR:
		return fmt.Sprintf("ERROR %v %v", r.id, r.description)
	case code.OPTIONS:
		return fmt.Sprintf("OPTIONS %v %v", r.id, r.servers)
	default:
		return fmt.Sprintf("UNKNOWN command: %v", fmt.Sprint(r.code))
	}
}

func (c commandID) String() string {
	return fmt.Sprintf("%v-%v-%v", c.serverID, c.connectionID, c.requestID)
}

func (server server) String() string {
	return fmt.Sprintf("%v-%v fails:%v", server.id, server.addr, server.fails)
}

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
