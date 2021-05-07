package util

import (
	"time"
)

const (
	nanosPerMilli = 1000000
)

// UnixMillis gives the milliseconds since epoch for the given time
func UnixMillis(t time.Time) int64 {
	return t.UnixNano() / nanosPerMilli
}

// NowUnixMillis gives now as milliseconds since epoch
func NowUnixMillis() int64 {
	return UnixMillis(time.Now())
}

// TimeFromMillis returns the time corresponding to the given milliseconds since epoch
func TimeFromMillis(millis int64) time.Time {
	return time.Unix(0, millis*nanosPerMilli)
}

// DurationSince returns the duration since the given time in millis
func DurationSince(millis int64) time.Duration {
	return time.Duration(int64(time.Now().Sub(TimeFromMillis(millis))))
}
