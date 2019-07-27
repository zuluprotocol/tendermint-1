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
	input   chan Event
	output  chan Event
	stopped chan struct{}
}

func newScheduler(output chan Event) *scheduler {
	input := make(chan Event, 1)
	return &scheduler{
		input:  input,
		output: output,
	}
}

func (sc *scheduler) run() {
	fmt.Println("scheduler run")
	for {
		iEvent, ok := <-sc.input
		if !ok {
			fmt.Println("stopping scheduler")
			sc.stopped <- struct{}{}
			break
		}
		oEvents := sc.handle(iEvent)
		for _, event := range oEvents {
			sc.output <- event
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

func (sc *scheduler) stop() {
	fmt.Println("scheduler stop")
	close(sc.input)
	<-sc.stopped
}

// processor
type processor struct {
	input   chan Event
	output  chan Event
	stopped chan struct{}
}

func newProcessor(output chan Event) *processor {
	input := make(chan Event, 1)
	return &processor{
		input:  input,
		output: output,
	}
}

func (pc *processor) run() {
	fmt.Println("processor run")
	for {
		iEvent, ok := <-pc.input
		if !ok {
			fmt.Println("stopping processor")
			pc.stopped <- struct{}{}
			break
		}

		oEvents := pc.handle(iEvent)
		for _, event := range oEvents {
			pc.output <- event
		}
	}
}

func (pc *processor) send(event Event) bool {
	fmt.Println("processor send")
	select {
	case pc.input <- event:
		return true
	default:
		fmt.Println("processor channel was full")
		return false
	}
}

func (pc *processor) handle(event Event) Events {
	switch event.(type) {
	case timeCheck:
		fmt.Println("processor handle timeCheck")
	case testEvent:
		fmt.Println("processor handle testEvent")
	}
	return Events{}
}

func (pc *processor) stop() {
	fmt.Println("processor stop")
	close(pc.input)
	<-pc.stopped
}

// demuxer
type demuxer struct {
	input     chan Event
	output    chan Event
	scheduler *scheduler
	processor *processor
	stopped   chan struct{}
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
		// so now we need a way to flush
		event, ok := <-dm.input
		if !ok {
			fmt.Println("demuxer stopping")
			dm.stopped <- struct{}{}
			break
		}
		// event.time = time.Now()
		received := dm.scheduler.send(event)
		if !received {
			panic("couldn't send to scheduler")
		}

		received = dm.processor.send(event)
		if !received {
			panic("couldn't send to the processor")
		}
	}
}

func (dm *demuxer) send(event Event) bool {
	fmt.Println("demuxer send")
	// we need to close if this is closed first
	select {
	case dm.input <- event:
		return true
	default:
		fmt.Println("demuxer channel was full")
		return false
	}
}

func (dm *demuxer) stop() {
	fmt.Println("demuxer stop")
	close(dm.input)
	<-dm.stopped
	fmt.Println("demuxer stopped")
}

// reactor

type DummyReactor struct {
	events        chan Event
	demuxer       *demuxer
	scheduler     *scheduler
	processor     *processor
	ticker        *time.Ticker
	tickerStopped chan struct{}
}

func (dr *DummyReactor) Start() {
	bufferSize := 10
	events := make(chan Event, bufferSize)

	dr.scheduler = newScheduler(events)
	dr.processor = newProcessor(events)
	dr.demuxer = newDemuxer(events, dr.scheduler, dr.processor)
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

// XXX: We need to have a smooth shutdown process
func (dr *DummyReactor) Stop() {
	fmt.Println("reactor stopping")
	// this should be synchronous
	dr.tickerStopped <- struct{}{}
	fmt.Println("waiting for ticker")
	// the order here matters
	dr.demuxer.stop() // this need to drain first
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
