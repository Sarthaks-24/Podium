package config

import (
	"net"
	"net/url"
)

// Mode is how strictly a service runs (PODIUM_MODE).
type Mode string

const (
	// ModeTesting runs without Google sign-in: anyone at this machine can
	// sign in as any person and role from the sign-in page. Everything
	// listens on loopback only, and no secret needs to be configured.
	ModeTesting Mode = "testing"
	// ModeDev is local development with real Google sign-in. Secrets for
	// sign-in are required; the judge may use the built-in testing token.
	ModeDev Mode = "dev"
	// ModeProduction requires every secret, an https base URL, and a real
	// judge token. It is the default, so a missing PODIUM_MODE is strict.
	ModeProduction Mode = "production"
)

// TestingJudgeToken is the worker token testing and dev modes use when
// PODIUM_JUDGE_TOKEN is not set, so the server and a local judge pair up
// without any configuration. It is public (it is in the source) and only
// ever accepted on a loopback listener; production refuses it.
const TestingJudgeToken = "podium-testing-token-public-never-use-in-production"

// testingOwner is the owner account in testing mode when PODIUM_OWNER_EMAIL
// is not set.
const testingOwner = "owner@podium.test"

func (r *reader) mode(name string) Mode {
	switch m := Mode(r.str(name)); m {
	case ModeTesting, ModeDev, ModeProduction:
		return m
	default:
		r.p.add("%s must be testing, dev or production", name)
		return ModeProduction
	}
}

// loopbackURL reports whether a base URL's host is this machine.
func loopbackURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
