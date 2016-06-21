package netmutex

import (
	"fmt"
	"os"
)

func (c command) String() string {
	switch c.code {
	case LOCK:
		return fmt.Sprintf("LOCK %v %v %v", c.key, c.id, c.ttl)
	case UNLOCK:
		return fmt.Sprintf("UNLOCK %v", c.id)
	case PING:
		return fmt.Sprintf("PING %v", c.id)
	case CONNECT:
		return "CONNECT"
	default:
		return fmt.Sprintf("UNKNOWN comand: %v", fmt.Sprint(c.code))
	}
}

func (r response) String() string {
	switch r.code {
	case OK:
		return fmt.Sprintf("OK %v", r.id)
	case REDIRECT:
		return fmt.Sprintf("REDIRECT %v to %v", r.id, r.node_id)
	case TIMEOUT:
		return fmt.Sprintf("TIMEOUT %v", r.id)
	case ERROR:
		return fmt.Sprintf("ERROR %v %v", r.id, r.description)
	case PONG:
		return fmt.Sprintf("PONG %v", r.id)
	case OPTIONS:
		return fmt.Sprintf("OPTIONS %v", r.id)
	default:
		return fmt.Sprintf("UNKNOWN comand: %v", fmt.Sprint(r.code))
	}
}

func (c commandId) String() string {
	return fmt.Sprintf("%v-%v-%v", c.node_id, c.connection_id, c.request_id)
}

func (node node) String() string {
	return fmt.Sprintf("%v-%v fails:%v mtu:%v rtt:%v", node.id, node.addr, node.fails, node.mtu, node.rtt)
}

func warn(a ...interface{}) {
	fmt.Fprintln(os.Stderr, a)
}

func say(a ...interface{}) {
	fmt.Println(a)
}
