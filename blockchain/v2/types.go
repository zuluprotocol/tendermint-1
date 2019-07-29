package v2

import "time"

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

type errEvent struct{}

type scTestEvent struct{}

type pcFinished struct{}
