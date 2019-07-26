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
type timeCheck struct {
	time time.Time
}

type handler struct {
	input  chan Event
	output chan Event
}

// base handler
func (hd *handler) run() {
	for {
		iEvent := <-hd.input
		stop, ok := iEvent.(stopEvent)
		if ok {
			fmt.Println("stopping handler")
			break
		}
		oEvents := hd.handle(iEvent)
		for _, event := range oEvents {
			hd.output <- event
		}
	}
}

func (hd *handler) handle(even Event) Events {
	fmt.Println("handler stand in handle")

	return Events{}
}

func (hd *handler) stop() {
	// XXX: What if this is full?
	hd.input <- struct{}{}
}

func (fs *handler) send(event Event) bool {
	select {
	case fs.input <- event:
		return true
	default:
		return false
	}
}

// scheduler

type scheduler struct {
	handler
}

func newScheduler(output chan Event) *scheduler {
	input := make(chan Event)
	handler := handler{
		input:  input,
		output: output,
	}
	return &scheduler{
		handler: handler,
	}
}

func (sc *scheduler) handle(event Event) Events {
	switch event := event.(type) {
	case timeCheck:
		fmt.Println("scheduler handle timeCheck")
	case testEvent:
		fmt.Println("scheduler handle testEvent")
	}
	return Events{}
}

// processor
type processor struct {
	handler
}

func newProcessor(output chan Event) *processor {
	handler := handler{output: output}
	return &processor{
		handler: handler,
	}
}

func (sc *processor) handle(event Event) Events {
	switch event := event.(type) {
	case timeCheck:
		fmt.Println("processor handle evTimeCheck")
	case testEvent:
		fmt.Println("processor handle testEvent")
	}
	return Events{}
}

// demuxer
type demuxer struct {
	handler
	scheduler *scheduler
	processor *processor
}

func newDemuxer(output chan Event, scheduler *scheduler, processor *processor) *demuxer {
	input := make(chan Event)
	handler := handler{
		input:  input,
		output: output,
	}
	return &demuxer{
		handler:   handler,
		scheduler: scheduler,
		processor: processor,
	}
}

func (dm *demuxer) run() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := timeCheck{time: time.Now()}
			dm.input <- now
		case event := <-dm.input:
			// event.time = time.Now()
			received := dm.scheduler.send(event)
			if !received {
				panic("couldn't send to scheduler")
			}

			received = dm.processor.send(event)
			if !received {
				panic("couldn't send to the processor")
			}

			_, ok := event.(stopEvent)
			if ok {
				fmt.Println("demuxer stopping")
				break
			}
		}
	}
}

// reactor

type DummyReactor struct {
	events  chan Event
	demuxer *demuxer
}

func (dr *DummyReactor) Start() {
	bufferSize := 10
	events := make(chan Event, bufferSize)

	scheduler := newScheduler(events)
	processor := newProcessor(events)
	dr.demuxer = newDemuxer(events, scheduler, processor)

	go scheduler.run()
	go processor.run()
	go dr.demuxer.run()
}

func (dr *DummyReactor) Stop() {
	_ = dr.demuxer.send(stopEvent{})
}

func (dr *DummyReactor) Receive(event Event) {
	sent := dr.demuxer.send(event)
	if !sent {
		panic("demuxer is full")
	}
}

func (dr *DummyReactor) AddPeer() {
	// TODO: add peer event and send to demuxer
}
