package v2

import (
	"testing"
)

func TestReactor(t *testing.T) {
	reactor = DummyReactor{}
	reactor.Start()
	script := Events{
		testEvent{},
	}

	for _, event := range script {
		reactor.Receive(event)
	}
	reactor.Stop()
}

/*
* Can we send a message to all routines
* Can routines emit events
* can we start routines
* can we stop all routines
 */
