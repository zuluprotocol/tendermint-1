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

// XXX: what about state here, we need per routine state
// So handle func should take an event and a reference to state and return
// events and the new state
type handleFunc = func(event Event) Events

// Routine
type Routine struct {
	name    string
	input   chan Event
	errors  chan errEvent
	output  chan Event
	stopped chan struct{}
	handle  handleFunc
}

func newRoutine(name string, output chan Event, handleFunc handleFunc) *Routine {
	return &Routine{
		name:    name,
		input:   make(chan Event, 1),
		errors:  make(chan errEvent, 1),
		output:  output,
		stopped: make(chan struct{}, 1),
		handle:  handleFunc,
	}
}

type errEvent struct{}

// run thriough input an errors giving errors a chance to skip the queue
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

func (rt *Routine) send(event Event) bool {
	fmt.Printf("%s: send\n", rt.name)
	if err, ok := event.(errEvent); ok {
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

type scTestEvent struct{}

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

type pcFinished struct{}

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

type demuxer struct {
	eventbus  chan Event
	scheduler *Routine
	processor *Routine
	finished  chan struct{}
	stopped   chan struct{}
}

func newDemuxer(scheduler *Routine, processor *Routine) *demuxer {
	return &demuxer{
		eventbus:  make(chan Event, 10),
		scheduler: scheduler,
		processor: processor,
		stopped:   make(chan struct{}, 1),
		finished:  make(chan struct{}, 1),
	}
}

func (dm *demuxer) run() {
	fmt.Printf("demuxer: run\n")
	for {
		select {
		case event, ok := <-dm.eventbus:
			if !ok {
				fmt.Printf("demuxer: stopping\n")
				dm.stopped <- struct{}{}
				return
			}
			oEvents := dm.handle(event)
			for _, event := range oEvents {
				dm.eventbus <- event
			}
		case event, ok := <-dm.scheduler.output:
			if !ok {
				fmt.Printf("demuxer: scheduler output closed\n")
				continue
				// todo: close?
			}
			oEvents := dm.handle(event)
			for _, event := range oEvents {
				dm.eventbus <- event
			}
		case event, ok := <-dm.processor.output:
			if !ok {
				fmt.Printf("demuxer: pricessor output closed\n")
				continue
				// todo: close?
			}
			oEvents := dm.handle(event)
			for _, event := range oEvents {
				dm.eventbus <- event
			}
		}
	}
}

type scFull struct{}
type pcFull struct{}

// XXX: What is the corerct behaviour here?
// onPcFinish, process no further events
// OR onPcFinish, process all queued events and then close
func (dm *demuxer) handle(event Event) Events {
	switch event.(type) {
	case pcFinished:
		// dm.stop()
		fmt.Println("demuxer received pcFinished")
		dm.finished <- struct{}{}
	default:
		received := dm.scheduler.send(event)
		if !received {
			return Events{scFull{}} // backpressure
		}

		received = dm.processor.send(event)
		if !received {
			return Events{pcFull{}} // backpressure
		}

		return Events{}
	}
	return Events{}
}

func (dm *demuxer) send(event Event) bool {
	fmt.Printf("demuxer send\n")
	select {
	case dm.eventbus <- event:
		return true
	default:
		fmt.Printf("demuxer channel was full\n")
		return false
	}
}

func (dm *demuxer) stop() {
	fmt.Printf("demuxer stop\n")
	close(dm.eventbus)
	<-dm.stopped
}

// reactor
type DummyReactor struct {
	events        chan Event
	demuxer       *demuxer
	scheduler     *Routine
	processor     *Routine
	ticker        *time.Ticker
	tickerStopped chan struct{}
	completed     chan struct{}
}

func (dr *DummyReactor) Start() {
	bufferSize := 10
	events := make(chan Event, bufferSize)

	dr.completed = make(chan struct{}, 1)

	dr.scheduler = newRoutine("scheduler", events, schedulerHandle)
	dr.processor = newRoutine("processor", events, processorHandle)
	dr.demuxer = newDemuxer(dr.scheduler, dr.processor)
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

func (dr *DummyReactor) Wait() {
	<-dr.demuxer.finished // maybe put this in a wait method
	fmt.Println("completed routines")
	dr.Stop()
}

func (dr *DummyReactor) Stop() {
	fmt.Println("reactor stopping")

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
