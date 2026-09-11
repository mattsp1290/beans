package issue

import "testing"

func TestRequestIDsAndTransitions(t *testing.T) {
	if !ValidRequestID("beans-r-a3f2") || ValidRequestID("beans-a3f2") || ValidRequestID("beans-r-") {
		t.Fatal("unexpected request id validation")
	}
	if got := NewRequestID("beans", func(string) bool { return false }, 7); len(got) != len("beans-r-")+7 {
		t.Fatalf("NewRequestID length = %d, want %d (%q)", len(got), len("beans-r-")+7, got)
	}
	statuses := []string{RequestOpen, RequestAccepted, RequestInProgress, RequestResolved, RequestDeclined}
	for _, from := range statuses {
		for _, to := range statuses {
			err := ValidateRequestTransition(from, to)
			allowed := from == to || from == RequestOpen && (to == RequestAccepted || to == RequestDeclined) || from == RequestAccepted && (to == RequestInProgress || to == RequestDeclined) || from == RequestInProgress && (to == RequestResolved || to == RequestDeclined)
			if (err == nil) != allowed {
				t.Errorf("%s -> %s: err=%v, allowed=%v", from, to, err, allowed)
			}
		}
	}
}
