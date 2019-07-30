package v2

import (
	"fmt"
)

// TODO
// break out routines
// logging
// metrics

type handleFunc = func(event Event) Events

// Routine
type Routine struct {
	name     string
	input    chan Event
	errors   chan error
	output   chan Event
	stopped  chan struct{}
	finished chan struct{}
	handle   handleFunc
}

func newRoutine(name string, output chan Event, handleFunc handleFunc) *Routine {
	return &Routine{
		name:     name,
		input:    make(chan Event, 1),
		errors:   make(chan error, 1),
		output:   output,
		stopped:  make(chan struct{}, 1),
		finished: make(chan struct{}, 1),
		handle:   handleFunc,
	}
}

func (rt *Routine) run() {
	fmt.Printf("%s: run\n", rt.name)
	for {
		select {
		case iEvent, ok := <-rt.input:
			if !ok {
				fmt.Printf("%s: stopping\n", rt.name)
				rt.stopped <- struct{}{}
				return
			}
			oEvents := rt.handle(iEvent)
			fmt.Printf("%s handled %d events\n", rt.name, len(oEvents))
			for _, event := range oEvents {
				// check for finished
				if _, ok := event.(routineFinished); ok {
					fmt.Printf("%s: finished\n", rt.name)
					rt.finished <- struct{}{}
					return
				}
				fmt.Println("writting back to output")
				rt.output <- event
			}
		case iEvent, ok := <-rt.errors:
			if !ok {
				fmt.Printf("%s: errors closed\n", rt.name)
				continue
			}
			oEvents := rt.handle(iEvent)
			fmt.Printf("%s handled %d events from errors\n", rt.name, len(oEvents))
			for _, event := range oEvents {
				rt.output <- event
			}
		}
	}
}
func (rt *Routine) feedback() {
	for event := range rt.output {
		rt.send(event)
	}
}

func (rt *Routine) send(event Event) bool {
	fmt.Printf("%s: send\n", rt.name)
	if err, ok := event.(error); ok {
		select {
		case rt.errors <- err:
			return true
		default:
			fmt.Printf("%s: errors channel was full\n", rt.name)
			return false
		}
	} else {
		select {
		case rt.input <- event:
			return true
		default:
			fmt.Printf("%s: channel was full\n", rt.name)
			return false
		}
	}
}

func (rt *Routine) stop() {
	fmt.Printf("%s: stop\n", rt.name)
	close(rt.errors)
	close(rt.input)
	<-rt.stopped
}

// XXX: this should probably produced the finished
// channel and let the caller deicde how long to wait
func (rt *Routine) wait() {
	<-rt.finished
}

func schedulerHandle(event Event) Events {
	switch event.(type) {
	case timeCheck:
		fmt.Println("scheduler handle timeCheck")
	case testEvent:
		fmt.Println("scheduler handle testEvent")
		return Events{scTestEvent{}}
	}
	return Events{}
}

func processorHandle(event Event) Events {
	switch event.(type) {
	case timeCheck:
		fmt.Println("processor handle timeCheck")
	case testEvent:
		fmt.Println("processor handle testEvent")
	case scTestEvent:
		fmt.Println("processor handle scTestEvent")
		// should i stop myself?
		return Events{pcFinished{}}
	}
	return Events{}
}
