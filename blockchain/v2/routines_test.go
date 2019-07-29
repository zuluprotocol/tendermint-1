package v2

import (
	"testing"
)

func TestReactor(t *testing.T) {
	reactor := DummyReactor{}
	reactor.Start()
	script := Events{
		testEvent{},
	}

	for _, event := range script {
		reactor.Receive(event)
	}
	reactor.Wait()
}
