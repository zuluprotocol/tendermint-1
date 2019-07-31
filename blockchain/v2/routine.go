package v2

import (
	"fmt"
	"sync/atomic"
)

// TODO
// logging
// metrics

type handleFunc = func(event Event) (Events, error)

// XXX: This should comply with the BaseService interface
// Routine
type Routine struct {
	name     string
	input    chan Event
	errors   chan error
	output   chan Event
	stopped  chan struct{}
	finished chan error
	running  *uint32
	handle   handleFunc
}

func newRoutine(name string, output chan Event, handleFunc handleFunc) *Routine {
	return &Routine{
		name:     name,
		input:    make(chan Event, 1),
		errors:   make(chan error, 1),
		output:   output,
		stopped:  make(chan struct{}, 1),
		finished: make(chan error, 1),
		running:  new(uint32),
		handle:   handleFunc,
	}
}

func (rt *Routine) run() {
	fmt.Printf("%s: run\n", rt.name)
	starting := atomic.CompareAndSwapUint32(rt.running, uint32(0), uint32(1))
	if !starting {
		panic("Routine has already started")
	}
	errorsDrained := false
	for {
		if !rt.isRunning() {
			break
		}
		select {
		case iEvent, ok := <-rt.input:
			if !ok {
				if !errorsDrained {
					continue // wait for errors to be drainned
				}
				fmt.Printf("%s: stopping\n", rt.name)
				rt.stopped <- struct{}{}
				return
			}
			oEvents, err := rt.handle(iEvent)
			if err != nil {
				rt.terminate(err)
				return
			}

			fmt.Printf("%s handled %d events\n", rt.name, len(oEvents))
			for _, event := range oEvents {
				fmt.Println("writting back to output")
				rt.output <- event
			}
		case iEvent, ok := <-rt.errors:
			if !ok {
				fmt.Printf("%s: errors closed\n", rt.name)
				errorsDrained = true
				continue
			}
			// rename flush?
			oEvents, err := rt.handle(iEvent)
			if err != nil {
				rt.terminate(err)
				return
			}
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
	// XXX: What if we arn't running? this will panic
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

func (rt *Routine) isRunning() bool {
	return atomic.LoadUint32(rt.running) == 1
}

// rename flush?
func (rt *Routine) stop() {
	fmt.Printf("%s: stop\n", rt.name)
	close(rt.input)
	close(rt.errors)
	<-rt.stopped // stuck here
	rt.terminate(fmt.Errorf("routine stopped"))
}

func (rt *Routine) terminate(reason error) {
	stopped := atomic.CompareAndSwapUint32(rt.running, uint32(1), uint32(0))
	if !stopped {
		panic("called stop but already stopped")
	}
	rt.finished <- reason
}

// XXX: this should probably produced the finished
// channel and let the caller deicde how long to wait
func (rt *Routine) wait() error {
	return <-rt.finished
}
