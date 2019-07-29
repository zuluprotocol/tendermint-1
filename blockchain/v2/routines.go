package v2

import (
	"fmt"
	"time"
)

/*
TODO:
	* Look at refactoring routines
		* single struct, paramterize the handle function
	* introduce an sendError and seperate error channel
	* How could this be tested?
		* Ensure Start/Stopping
		* Ensure all messages sent are processed
		* Ensure that errors can be processed depsite outstanding messages
*/
type testEvent struct {
	msg  string
	time time.Time
}

type testEventTwo struct {
	msg string
}

type stopEvent struct{}
type timeCheck struct {
	time time.Time
}

type handleFunc = func(event Event) Events

// Routine
type Routine struct {
	name    string
	input   chan Event
	output  chan Event
	stopped chan struct{}
	handle  handleFunc
}

func newRoutine(name string, output chan Event, handleFunc handleFunc) *Routine {
	return &Routine{
		name:    name,
		input:   make(chan Event, 1),
		output:  output,
		stopped: make(chan struct{}, 1),
		handle:  handleFunc,
	}
}

// XXX: what about error handling?
// we need an additional error channel here which will ensure
// errors get processed as soon as possible
// XXX: what about state here, we need per routine state
func (rt *Routine) run() {
	fmt.Printf("%s: run\n", rt.name)
	for {
		iEvent, ok := <-rt.input
		if !ok {
			fmt.Printf("%s: stopping\n", rt.name)
			rt.stopped <- struct{}{}
			break
		}
		oEvents := rt.handle(iEvent)
		for _, event := range oEvents {
			rt.output <- event
		}
	}
}

func (rt *Routine) send(event Event) bool {
	fmt.Printf("%s: send\n", rt.name)
	select {
	case rt.input <- event:
		return true
	default:
		fmt.Printf("%s: channel was full\n", rt.name)
		return false
	}
}

func (rt *Routine) stop() {
	fmt.Printf("%s: stop\n", rt.name)
	close(rt.input)
	<-rt.stopped
}

func schedulerHandle(event Event) Events {
	switch event.(type) {
	case timeCheck:
		fmt.Println("scheduler handle timeCheck")
	case testEvent:
		fmt.Println("scheduler handle testEvent")
	}
	return Events{}
}

func processorHandle(event Event) Events {
	switch event.(type) {
	case timeCheck:
		fmt.Println("processor handle timeCheck")
	case testEvent:
		fmt.Println("processor handle testEvent")
	}
	return Events{}
}

func genDemuxerHandle(scheduler *Routine, processor *Routine) handleFunc {
	return func(event Event) Events {
		received := scheduler.send(event)
		if !received {
			panic("couldn't send to scheduler")
		}

		received = processor.send(event)
		if !received {
			panic("couldn't send to the processor")
		}

		// XXX: think about emitting backpressure if !received
		return Events{}
	}
}

// reactor
type DummyReactor struct {
	events        chan Event
	demuxer       *Routine
	scheduler     *Routine
	processor     *Routine
	ticker        *time.Ticker
	tickerStopped chan struct{}
}

func (dr *DummyReactor) Start() {
	bufferSize := 10
	events := make(chan Event, bufferSize)

	dr.scheduler = newRoutine("scheduler", events, schedulerHandle)
	dr.processor = newRoutine("processor", events, processorHandle)
	demuxerHandle := genDemuxerHandle(dr.scheduler, dr.processor)
	dr.demuxer = newRoutine("demuxer", events, demuxerHandle)
	dr.tickerStopped = make(chan struct{})

	go dr.scheduler.run()
	go dr.processor.run()
	go dr.demuxer.run()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-ticker.C:
				dr.demuxer.send(timeCheck{})
			case <-dr.tickerStopped:
				fmt.Println("ticker stopped")
				return
			}
		}
	}()
}

func (dr *DummyReactor) Stop() {
	fmt.Println("reactor stopping")
	// this should be synchronous
	dr.tickerStopped <- struct{}{}
	dr.demuxer.stop()
	dr.scheduler.stop()
	dr.processor.stop()

	fmt.Println("reactor stopped")
}

func (dr *DummyReactor) Receive(event Event) {
	fmt.Println("receive event")
	sent := dr.demuxer.send(event)
	if !sent {
		panic("demuxer is full")
	}
}

func (dr *DummyReactor) AddPeer() {
	// TODO: add peer event and send to demuxer
}
