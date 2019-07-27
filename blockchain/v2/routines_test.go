package v2

import (
	"fmt"
	"testing"
	"time"
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
	fmt.Println("sleeping")
	time.Sleep(5 * time.Second)
	reactor.Stop()
}
