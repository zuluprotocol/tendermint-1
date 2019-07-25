package v2

import (
	"fmt"
	"time"
)

/*
# Message passing between components
	* output of one routine becomes input for all other routines
	* avoid loops somehow
	* Message priority

# Components have isolated lifecycle management
	* Lifecycle management
		* Setup
		* Teardown
# Individual
			* message passing should be non blocking
		* backpressure between components
		* Lifecycle management of components
		* Observable behavior:
			* progress
			* blocking components
	What would look a test look like?
		Lifecycle management
			Start/Stop

	How to make this non blocking?

	How to avoid Thread saturation
	How to handle concurrency
*/

type Event interface{}
type Events []Event

type testEvent struct {
	msg  string
	time time.Time
}

type testEventTwo struct {
	msg string
}

type stopEvent struct{}

func demuxRoutine(msgs, scheduleMsgs, processorMsgs, ioMsgs) {
	for {
		select {
		case <-timer.C:
			now := evTimeCheck{time.Now()}
			schedulerMsgs <- now
			processorMsgs <- now
			ioMsgs <- now
		case msg := <-msgs:
			msg.time = time.Now()
			// These channels should produce backpressure before
			// being full to avoid starving each other
			schedulerMsgs <- msg
			processorMsgs <- msg
			ioMesgs <- msg

			stop, ok := msg.(type)
			if ok {
				fmt.Println("demuxer stopping")
				break
			}
		}
	}
}

func processorRoutine(input chan Event, output chan Event) {
	for {
		msg := <-input
		switch msg := msg.(type) {
		case testEvent:
			fmt.Println("processor testEvent")
			//output <- processor.handleBlockRequest(msg))
		case stopEvent:
			fmt.Println("stop processor")
			break
		}
	}
}

func schedulerRoutine(input chan Event, output chan Event) {
	for {
		msg := <-msgs
		switch msg := msg.(type) {
		case testEvent:
			fmt.Println("processor testEvent")
			//output <- processor.handleBlockRequest(msg))
		case stopEvent:
			fmt.Println("stop processor")
			break
		}
	}
}

func ioRoutine(input chan Event, output chan Event) {
	for {
		msg := <-msgs
		switch msg := msg.(type) {
		case testEvent:
			fmt.Println("processor testEvent")
			//output <- processor.handleBlockRequest(msg))
		case stopEvent:
			fmt.Println("stop processor")
			break
		}
	}
}

type DummyReactor struct {
	timer    timeTicker
	eventsCh chan Event
}

func (dr *DummyReactor) Start() {
	timer := time.NewTicker(interval)
}

func (dr *DummyReactor) Stop() {}

func (dr *DummyReactor) Receive(event Event) {}

// can we make a main function here and run some tests?
