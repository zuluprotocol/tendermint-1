package v2

import (
	"fmt"
	"time"
)

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
