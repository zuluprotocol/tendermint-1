package v2

import (
	"testing"
	"time"
)

type eventA struct{}
type eventB struct{}

func simpleHandler(event Event) Events {
	switch event.(type) {
	case eventA:
		return Events{eventB{}}
	case eventB:
		return Events{routineFinished{}}
	}
	return Events{}
}

func TestRoutine(t *testing.T) {
	events := make(chan Event, 10)
	routine := newRoutine("simpleRoutine", events, simpleHandler)

	go routine.run()
	go routine.feedback()

	routine.send(eventA{})

	routine.wait()
}

type incEvent struct{}

func genStatefulHandler(maxCount int) handleFunc {
	counter := 0
	return func(event Event) Events {
		switch event.(type) {
		case eventA:
			counter += 1
			if counter >= maxCount {
				return Events{routineFinished{}}
			}

			return Events{eventA{}}
		}
		return Events{}
	}
}

func TestStatefulRoutine(t *testing.T) {
	events := make(chan Event, 10)
	handler := genStatefulHandler(10)
	routine := newRoutine("statefulRoutine", events, handler)

	go routine.run()
	go routine.feedback()

	routine.wait()
}

func handleWithErrors(event Event) Events {
	switch event.(type) {
	case eventA:
		return Events{}
	case errEvent:
		return Events{routineFinished{}}
	}
	return Events{}
}

func TestErrorSaturation(t *testing.T) {
	events := make(chan Event, 10)
	routine := newRoutine("statefulRoutine", events, handleWithErrors)

	go routine.run()
	go func() {
		for {
			routine.send(eventA{})
			time.Sleep(10 * time.Millisecond)
		}
	}()
	routine.send(errEvent{})

	routine.wait()
}
