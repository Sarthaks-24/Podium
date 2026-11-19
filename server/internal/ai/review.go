package ai

// Review of an AI answer before any problem exists.

import "github.com/Sarthaks-24/podium/internal/problems"

// answerPack is a checked answer ready to become a draft: the draft, the
// model's solutions for the proof and whether it is a function problem.
type answerPack struct {
	draft     problems.Draft
	solutions map[string]string
	function  bool
}
