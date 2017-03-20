package netmutex

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/MichaelMonashev/sync/netmutex/code"
)

func (req request) String() string {
	switch req.code {
	case code.LOCK:
		return fmt.Sprintf("LOCK %v %v %v", req.key, req.id, req.ttl)
	case code.UPDATE:
		return fmt.Sprintf("UPDATE %v %v %v %v", req.key, req.id, req.lockID, req.ttl)
	case code.UNLOCK:
		return fmt.Sprintf("UNLOCK %v %v %v", req.key, req.id, req.lockID)
	case code.UNLOCKALL:
		return fmt.Sprintf("UNLOCKALL %v", req.id)
	case code.PING:
		return fmt.Sprintf("PING %v", req.id)
	case code.CONNECT:
		return "CONNECT"
	default:
		return fmt.Sprintf("UNKNOWN command: %v", fmt.Sprint(req.code))
	}
}

func (resp response) String() string {
	switch resp.code {
	case code.OK:
		return fmt.Sprintf("OK %v", resp.id)
	case code.REDIRECT:
		return fmt.Sprintf("REDIRECT %v to %v", resp.id, resp.serverID)
	case code.LOCKED:
		return fmt.Sprintf("LOCKED %v", resp.id)
	case code.ERROR:
		return fmt.Sprintf("ERROR %v %v", resp.id, resp.description)
	case code.OPTIONS:
		return fmt.Sprintf("OPTIONS %v %v", resp.id, resp.servers)
	default:
		return fmt.Sprintf("UNKNOWN command: %v", fmt.Sprint(resp.code))
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
