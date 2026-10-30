package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// A change with no actor on the context is refused, never taken as the
// server acting.
func TestChangeWithoutActorIsRefused(t *testing.T) {
	s := &Service{OwnerEmail: "owner@college.edu"}
	var ce *ChangeError
	if err := s.changeAllowed(context.Background(), "asha@college.edu", Student, Student); !errors.As(err, &ce) || ce.Kind != "forbidden" {
		t.Errorf("no actor: %v, want a forbidden refusal", err)
	}
}

// Every actor role against every kind of target and every role a People
// action can leave the target with (V4-16, stage O O2). The expected
// answer is written out from the stage file's table, not derived from the
// code under test.
func TestAllowedChangeEveryCase(t *testing.T) {
	const owner = "owner@college.edu"
	actors := map[Role]Actor{
		Student:   {ID: 10, Email: "s@college.edu", Role: Student},
		Organiser: {ID: 11, Email: "o@college.edu", Role: Organiser},
		Admin:     {ID: 12, Email: "a@college.edu", Role: Admin},
		Owner:     {ID: 13, Email: "w@college.edu", Role: Owner},
	}
	type target struct {
		name    string
		email   func(actor Actor) string
		current Role
	}
	targets := []target{
		{"no account yet", func(Actor) string { return "new@college.edu" }, ""},
		{"a student", func(Actor) string { return "st@college.edu" }, Student},
		{"an organiser", func(Actor) string { return "or@college.edu" }, Organiser},
		{"another admin", func(Actor) string { return "ad@college.edu" }, Admin},
		{"an owner", func(Actor) string { return "ow@college.edu" }, Owner},
		{"the configured owner", func(Actor) string { return owner }, Owner},
		{"themself", func(a Actor) string { return a.Email }, ""},
	}
	nexts := []Role{Student, Organiser, Admin, Owner}

	allowed := func(actor Role, tg target, next Role) bool {
		switch actor {
		case Owner:
			return true
		case Admin:
			if tg.name == "themself" || tg.name == "the configured owner" {
				return false
			}
			small := func(r Role) bool { return r == "" || r == Student || r == Organiser }
			return small(tg.current) && small(next)
		}
		return false
	}

	for actorRole, actor := range actors {
		for _, tg := range targets {
			current := tg.current
			if tg.name == "themself" {
				current = actorRole
			}
			for _, next := range append(nexts, current) {
				name := fmt.Sprintf("%s changes %s (%s) to %s", actorRole, tg.name, current, next)
				err := allowedChange(actor, owner, tg.email(actor), current, next)
				want := allowed(actorRole, tg, next)
				var ce *ChangeError
				switch {
				case want && err != nil:
					t.Errorf("%s: refused: %v", name, err)
				case !want && (!errors.As(err, &ce) || ce.Kind != "forbidden"):
					t.Errorf("%s: %v, want a forbidden refusal", name, err)
				}
			}
		}
	}
}
