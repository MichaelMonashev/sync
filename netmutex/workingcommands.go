package netmutex

import (
	"sync"
)

const workingCommandsSize = 16 // степь двойки . размер массива: 1 << 16 = 65536

type workingCommands struct {
	a [1 << workingCommandsSize]*busket
}

type busket struct {
	sync.Mutex
	m map[commandID]*request
}

func newWorkingCommands() *workingCommands {
	wc := &workingCommands{}

	for i := len(wc.a) - 1; i >= 0; i-- {
		wc.a[i] = &busket{
			m: make(map[commandID]*request),
		}
		//wc.a[i].m = make(map[commandID]*request)
	}

	return wc
}

func hash(commandID commandID) int {
	return int(commandID.connectionID+commandID.requestID) & (1<<workingCommandsSize - 1)
}

func (wc *workingCommands) add(req *request) {
	busket := wc.a[hash(req.id)]

	busket.Lock()
	defer busket.Unlock()

	busket.m[req.id] = req
}

func (wc *workingCommands) get(commandID commandID) (*request, bool) {
	busket := wc.a[hash(commandID)]

	busket.Lock()
	defer busket.Unlock()

	req, ok := busket.m[commandID]
	return req, ok
}

func (wc *workingCommands) delete(commandID commandID) {
	busket := wc.a[hash(commandID)]

	busket.Lock()
	defer busket.Unlock()

	delete(busket.m, commandID)
}
