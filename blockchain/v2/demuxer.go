package v2

import "fmt"

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
				fmt.Printf("demuxer: processor output closed\n")
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
