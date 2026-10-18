package events

import "testing"

func TestPublishReachesOnlyTheUser(t *testing.T) {
	var h Hub
	a1, _ := h.Subscribe(1)
	a2, _ := h.Subscribe(1)
	b, _ := h.Subscribe(2)
	h.Publish(1, "submission", map[string]int{"id": 7})
	for _, c := range []*Client{a1, a2} {
		select {
		case ev := <-c.C:
			if ev.Type != "submission" || string(ev.Data) != `{"id":7}` {
				t.Errorf("event %+v", ev)
			}
		default:
			t.Error("a tab of user 1 got nothing")
		}
	}
	select {
	case ev := <-b.C:
		t.Errorf("user 2 got %+v", ev)
	default:
	}
	if h.Connections() != 3 {
		t.Errorf("connections %d", h.Connections())
	}
	h.Unsubscribe(a1)
	h.Unsubscribe(a1) // twice is harmless
	if h.Connections() != 2 {
		t.Errorf("after unsubscribe: %d", h.Connections())
	}
}

func TestSlowClientIsDropped(t *testing.T) {
	var h Hub
	slow, _ := h.Subscribe(1)
	for i := 0; i < buffer+1; i++ {
		h.Publish(1, "tick", i)
	}
	select {
	case <-slow.Done:
	default:
		t.Fatal("a client that stopped reading was not dropped")
	}
	if h.Connections() != 0 {
		t.Errorf("connections %d", h.Connections())
	}
	h.Publish(1, "tick", 0) // publishing to nobody is fine
}

func TestStreamLimits(t *testing.T) {
	var h Hub
	var first *Client
	for i := 0; i < MaxPerUser+1; i++ {
		c, err := h.Subscribe(1)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = c
		}
	}
	select {
	case <-first.Done:
	default:
		t.Error("the oldest tab was not replaced")
	}
	if h.Connections() != MaxPerUser {
		t.Errorf("connections %d", h.Connections())
	}
}
