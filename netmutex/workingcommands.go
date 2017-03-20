package netmutex

import (
	"sync"
)

type workingCommands struct {
	sync.RWMutex
	m map[commandID]*request
}

func (wc *workingCommands) add(req *request) {
	wc.Lock()
	defer wc.Unlock()

	wc.m[req.id] = req
}

func (wc *workingCommands) get(commandID commandID) (*request, bool) {
	wc.RLock()
	defer wc.RUnlock()

	req, ok := wc.m[commandID]
	return req, ok
}

func (wc *workingCommands) delete(commandID commandID) {
	wc.Lock()
	defer wc.Unlock()

	delete(wc.m, commandID)
}
