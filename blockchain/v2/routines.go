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

// scheduler

type scheduler struct {
	input  chan Event
	output chan Event
}

func newScheduler(output chan Event) *scheduler {
	input := make(chan Event, 1)
	return &scheduler{
		input:  input,
		output: output,
	}
}

func (hd *scheduler) run() {
	fmt.Println("scheduler run")
	for {
		iEvent := <-hd.input
		_, ok := iEvent.(stopEvent)
		if ok {
			fmt.Println("stopping scheduler")
			break
		}
		oEvents := hd.handle(iEvent)
		for _, event := range oEvents {
			hd.output <- event
		}
	}
}

func (fs *scheduler) send(event Event) bool {
	fmt.Println("scheduler send")
	select {
	case fs.input <- event:
		return true
	default:
		fmt.Println("scheduler channel was full")
		return false
	}
}

func (sc *scheduler) handle(event Event) Events {
	switch event.(type) {
	case timeCheck:
		fmt.Println("scheduler handle timeCheck")
	case testEvent:
		fmt.Println("scheduler handle testEvent")
	}
	return Events{}
}

func (hd *scheduler) stop() {
	fmt.Println("scheduler stop")
	hd.input <- stopEvent{}
}

// processor
type processor struct {
	input  chan Event
	output chan Event
}

func newProcessor(output chan Event) *processor {
	input := make(chan Event, 1)
	return &processor{
		input:  input,
		output: output,
	}
}

func (hd *processor) run() {
	fmt.Println("processor run")
	for {
		iEvent := <-hd.input
		_, ok := iEvent.(stopEvent)
		if ok {
			fmt.Println("stopping processor")
			break
		}
		oEvents := hd.handle(iEvent)
		for _, event := range oEvents {
			hd.output <- event
		}
	}
}

func (fs *processor) send(event Event) bool {
	fmt.Println("processor send")
	select {
	case fs.input <- event:
		return true
	default:
		fmt.Println("processor channel was full")
		return false
	}
}

func (sc *processor) handle(event Event) Events {
	switch event.(type) {
	case timeCheck:
		fmt.Println("processor handle timeCheck")
	case testEvent:
		fmt.Println("processor handle testEvent")
	}
	return Events{}
}

func (hd *processor) stop() {
	fmt.Println("processor stop")
	hd.input <- stopEvent{}
}

// demuxer
type demuxer struct {
	input     chan Event
	output    chan Event
	scheduler *scheduler
	processor *processor
}

func newDemuxer(output chan Event, scheduler *scheduler, processor *processor) *demuxer {
	input := make(chan Event, 1)
	return &demuxer{
		input:     input,
		output:    output,
		scheduler: scheduler,
		processor: processor,
	}
}

func (dm *demuxer) run() {
	fmt.Println("Running demuxer")
	for {
		event := <-dm.input
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

func (fs *demuxer) send(event Event) bool {
	fmt.Println("demuxer send")
	select {
	case fs.input <- event:
		return true
	default:
		fmt.Println("demuxer channel was full")
		return false
	}
}

func (hd *demuxer) stop() {
	fmt.Println("demuxer stop")
	hd.input <- stopEvent{}
}

// reactor

type DummyReactor struct {
	events  chan Event
	demuxer *demuxer
	ticker  *time.Ticker
}

func (dr *DummyReactor) Start() {
	bufferSize := 10
	events := make(chan Event, bufferSize)

	scheduler := newScheduler(events)
	processor := newProcessor(events)
	dr.demuxer = newDemuxer(events, scheduler, processor)
	dr.ticker = time.NewTicker(1 * time.Second)

	go scheduler.run()
	go processor.run()
	go dr.demuxer.run()
	go func() {
		for t := range dr.ticker.C {
			dr.demuxer.send(timeCheck{t})
		}
	}()
}

func (dr *DummyReactor) Stop() {
	// this should be synchronous
	dr.ticker.Stop()
	dr.demuxer.stop()
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
