package events

import (
	"sort"
	"testing"
)

func TestPublishManyUsersAndTags(t *testing.T) {
	var h Hub
	a, _ := h.SubscribeTagged(1, "laptop")
	h.SubscribeTagged(1, "phone")
	b, _ := h.Subscribe(2)
	c, _ := h.Subscribe(3)
	h.PublishMany([]int64{1, 2}, "standings", map[string]int{"version": 4})
	for _, cl := range []*Client{a, b} {
		select {
		case ev := <-cl.C:
			if ev.Type != "standings" || string(ev.Data) != `{"version":4}` {
				t.Errorf("event %+v", ev)
			}
		default:
			t.Error("a listed user got nothing")
		}
	}
	select {
	case ev := <-c.C:
		t.Errorf("user 3 got %+v", ev)
	default:
	}
	users := h.Users()
	sort.Slice(users, func(i, j int) bool { return users[i] < users[j] })
	if len(users) != 3 || users[0] != 1 || users[2] != 3 {
		t.Errorf("users %v", users)
	}
	tags := h.Tags(1)
	sort.Strings(tags)
	if len(tags) != 2 || tags[0] != "laptop" || tags[1] != "phone" {
		t.Errorf("tags %v", tags)
	}
}

func TestReplacedStreamIsMarked(t *testing.T) {
	var h Hub
	var first *Client
	for i := 0; i < MaxPerUser; i++ {
		c, _ := h.Subscribe(1)
		if i == 0 {
			first = c
		}
	}
	if first.Replaced() {
		t.Fatal("marked before being replaced")
	}
	h.Subscribe(1)
	select {
	case <-first.Done:
	default:
		t.Fatal("the oldest stream was not closed")
	}
	if !first.Replaced() {
		t.Error("the replaced stream does not say so")
	}
	slow, _ := h.Subscribe(2)
	for i := 0; i <= buffer; i++ {
		h.Publish(2, "x", i)
	}
	if slow.Replaced() {
		t.Error("a slow stream is dropped, not replaced")
	}
}
