package yellowcard

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

// EventTime is a webhook timestamp sent as an ISO 8601 string or as Unix
// seconds. A value it cannot read decodes to the zero time rather than
// failing the whole event.
type EventTime struct {
	time.Time
}

// UnmarshalJSON accepts a quoted RFC 3339 timestamp, a number of seconds, or
// null.
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
	if secs, err := strconv.ParseFloat(string(data), 64); err == nil {
		t.Time = time.Unix(int64(secs), 0).UTC()
		return nil
	}
	t.Time = time.Time{}
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
	if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(secs, 0).UTC()
	}
	return time.Time{}
}
