package denoise

import (
	"testing"
)

func TestStormDetector_FiresAtThreshold(t *testing.T) {
	clock := int64(1000)
	s := NewStormDetector()
	s.now = func() int64 { return clock }

	const incidentId = 42

	if s.Observe(incidentId, 3, 60) {
		t.Fatalf("should not fire on 1st event")
	}
	if s.Observe(incidentId, 3, 60) {
		t.Fatalf("should not fire on 2nd event")
	}
	if !s.Observe(incidentId, 3, 60) {
		t.Fatalf("should fire on 3rd event (threshold reached)")
	}
}

func TestStormDetector_RefiresAfterReset(t *testing.T) {
	clock := int64(1000)
	s := NewStormDetector()
	s.now = func() int64 { return clock }

	const incidentId = 42

	for i := 0; i < 3; i++ {
		s.Observe(incidentId, 3, 60)
	}
	// counter has been reset; another 3 events should fire again.
	if s.Observe(incidentId, 3, 60) {
		t.Fatalf("should not fire on 4th event (count=1 after reset)")
	}
	if s.Observe(incidentId, 3, 60) {
		t.Fatalf("should not fire on 5th (count=2)")
	}
	if !s.Observe(incidentId, 3, 60) {
		t.Fatalf("should re-fire on 6th (count=3 after reset)")
	}
}

func TestStormDetector_OldEventsExpireOutOfWindow(t *testing.T) {
	clock := int64(1000)
	s := NewStormDetector()
	s.now = func() int64 { return clock }

	s.Observe(42, 3, 60) // t=1000
	s.Observe(42, 3, 60) // t=1000

	clock = 1100 // 100s later — both old events fall out of 60s window

	if s.Observe(42, 3, 60) {
		t.Fatalf("should not fire: old events expired, this is effectively the 1st in-window event")
	}
}

func TestStormDetector_DisabledThresholdReturnsFalse(t *testing.T) {
	s := NewStormDetector()
	for i := 0; i < 100; i++ {
		if s.Observe(42, 0, 60) {
			t.Fatalf("threshold=0 must never fire")
		}
	}
}

func TestStormDetector_ForgetClearsState(t *testing.T) {
	s := NewStormDetector()
	s.Observe(42, 3, 60)
	s.Observe(42, 3, 60)
	s.Forget(42)

	// Counter cleared — need full threshold again from scratch.
	if s.Observe(42, 3, 60) {
		t.Fatalf("after Forget, 1st event must not fire")
	}
}
