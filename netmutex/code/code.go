// Package code contain commands for clent-server interaction.
package code

// Client request codes:
const (
	CONNECT    = iota // requesting connection parameters
	PING              // checking the passing of packets to the server and back
	TOUCH             // updates the time of the last client activity
	DISCONNECT        // request to disconnect

	LOCK      // lock the key
	RLOCK     // lock the key for writing
	UPDATE    // update the key ttl
	UNLOCK    // urlock the key
	UNLOCKALL // remove all locks
)

// server answers
const (
	OPTIONS = iota // contain connection parameters
	PONG           // response to the PING request

	OK           // request done
	DISCONNECTED // the client was disconnected before the request
	ISOLATED     // the client was isolated. You need to quit the program
	LOCKED       // key was locked by someone else
	WORKING      // the request is processed and the response will be sent later
	REDIRECT     // repeat request to another server
	ERROR        // error with descroption
)

// flags of transport level
const (
	FRAGMENTED    = 1 << iota // one of several fragments
	LAST_FRAGMENT             // last fragment
	BUSY                      // server is overloaded, send fewer requests
)
