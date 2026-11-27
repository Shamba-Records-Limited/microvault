package yellowcard

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"time"
)

// millisThreshold separates Unix seconds from milliseconds: 1e12 seconds is
// the year 33658, 1e12 milliseconds is 2001.
const millisThreshold = 1e12

// EventTime is a webhook timestamp sent as an ISO 8601 string or as Unix
// seconds or milliseconds. A value it cannot read decodes to the zero time
// rather than failing the whole event.
type EventTime struct {
	time.Time
}

// UnmarshalJSON accepts a quoted RFC 3339 timestamp, a Unix number, or null.
func (t *EventTime) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		t.Time = time.Time{}
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		t.Time = parseEventTime(s)
		return nil
	}
	t.Time = unixTime(string(data))
	return nil
}

// MarshalJSON writes RFC 3339, or null for the zero time.
func (t EventTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.UTC().Format(time.RFC3339Nano))
}

func parseEventTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts.UTC()
	}
	return unixTime(s)
}

// unixTime reads seconds or milliseconds since the epoch. Anything negative,
// non-finite or beyond int64 milliseconds is unreadable.
func unixTime(s string) time.Time {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= math.MaxInt64 {
		return time.Time{}
	}
	if v >= millisThreshold {
		return time.UnixMilli(int64(v)).UTC()
	}
	return time.Unix(int64(v), 0).UTC()
}
