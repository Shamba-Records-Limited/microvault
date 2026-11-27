package yellowcard

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWebhookEvent_ExecutedAtForms(t *testing.T) {
	want := time.Date(2023, 2, 20, 14, 25, 30, 459000000, time.UTC)
	cases := map[string]struct {
		raw  string
		want time.Time
	}{
		"documented iso string": {`"2023-02-20T14:25:30.459Z"`, want},
		"unix seconds":          {`1735689600`, time.Unix(1735689600, 0).UTC()},
		"seconds as string":     {`"1735689600"`, time.Unix(1735689600, 0).UTC()},
		"unix milliseconds":     {`1735689600123`, time.UnixMilli(1735689600123).UTC()},
		"negative":              {`-5`, time.Time{}},
		"beyond int64":          {`1e300`, time.Time{}},
		"null":                  {`null`, time.Time{}},
		"unreadable string":     {`"yesterday"`, time.Time{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body := `{"id":"p-1","sequenceId":"s-1","status":"complete","event":"SEND.COMPLETE","executedAt":` + tc.raw + `}`
			var e WebhookEvent
			if err := json.Unmarshal([]byte(body), &e); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !e.ExecutedAt.Equal(tc.want) {
				t.Errorf("executedAt = %v, want %v", e.ExecutedAt.Time, tc.want)
			}
			if e.Event != EventSendComplete || e.PaymentID != "p-1" {
				t.Errorf("event = %+v", e)
			}
		})
	}
}

// The payload exactly as YellowCard documents it must decode.
func TestWebhookEvent_DocumentedPayload(t *testing.T) {
	body := `{
	  "id": "00e97bc4-1429-4ce7-acb5-841f9d9ed059",
	  "sequenceId": "1a051f68-5c55-44c8-b4a0-366800daaf19",
	  "status": "failed",
	  "apiKey": "513fc4c3aaeb2a8f292a740ea178d830",
	  "event": "RECEIVE.FAILED",
	  "errorCode": "REFUSED",
	  "sessionId":"a4cf97cd-8f1a-4e38-842b-e9597affb199",
	  "executedAt": "2023-02-20T14:25:30.459Z"
	}`
	var e WebhookEvent
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Event != EventReceiveFailed || e.ErrorCode != "REFUSED" || e.ExecutedAt.IsZero() {
		t.Errorf("event = %+v", e)
	}
}

func TestEventTime_MarshalRoundTrip(t *testing.T) {
	in := EventTime{time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out EventTime
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if !out.Equal(in.Time) {
		t.Errorf("round trip = %v, want %v", out.Time, in.Time)
	}
	if raw, _ := json.Marshal(EventTime{}); string(raw) != "null" {
		t.Errorf("zero = %s, want null", raw)
	}
}
