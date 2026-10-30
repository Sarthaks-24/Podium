//go:build fakeai

package main

// Builds for local testing (make dev, make dev-test) link the testing-mode
// AI provider and its canned packs; the release binary is built without
// the tag and has neither.
import _ "github.com/Sarthaks-24/podium/internal/ai/fakeai"
