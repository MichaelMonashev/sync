package netmutex

import (
	"sync"
)

type workingCommands struct {
	sync.RWMutex
	m map[commandID]*command
}

func (wc *workingCommands) add(command *command) {
	wc.Lock()
	defer wc.Unlock()

	wc.m[command.id] = command
}

func (wc *workingCommands) get(commandID commandID) (*command, bool) {
	wc.RLock()
	defer wc.RUnlock()

	command, ok := wc.m[commandID]
	return command, ok
}

func (wc *workingCommands) delete(commandID commandID) {
	wc.Lock()
	defer wc.Unlock()

	delete(wc.m, commandID)
}
