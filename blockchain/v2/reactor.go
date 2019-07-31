package v2

import (
	"fmt"
	"time"
)

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

// reactor
type Reactor struct {
	events        chan Event
	demuxer       *demuxer
	scheduler     *Routine
	processor     *Routine
	ticker        *time.Ticker
	tickerStopped chan struct{}
}

func (r *Reactor) Start() {
	bufferSize := 10
	events := make(chan Event, bufferSize)

	r.scheduler = newRoutine("scheduler", events, schedulerHandle)
	r.processor = newRoutine("processor", events, processorHandle)
	r.demuxer = newDemuxer(r.scheduler, r.processor)
	r.tickerStopped = make(chan struct{})

	go r.scheduler.run()
	go r.processor.run()
	go r.demuxer.run()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-ticker.C:
				// xxx: what if !sent?
				r.demuxer.send(timeCheck{})
			case <-r.tickerStopped:
				fmt.Println("ticker stopped")
				return
			}
		}
	}()
}

func (r *Reactor) Wait() {
	<-r.demuxer.finished // maybe put this in a wait method
	fmt.Println("completed routines")
	r.Stop()
}

func (r *Reactor) Stop() {
	fmt.Println("reactor stopping")

	r.tickerStopped <- struct{}{}
	r.demuxer.stop()
	r.scheduler.stop()
	r.processor.stop()
	// todo: accumulator
	// todo: io

	fmt.Println("reactor stopped")
}

func (r *Reactor) Receive(event Event) {
	fmt.Println("receive event")
	sent := r.demuxer.send(event)
	if !sent {
		panic("demuxer is full")
	}
}

func (r *Reactor) AddPeer() {
	// TODO: add peer event and send to demuxer
}
