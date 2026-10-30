package auth

import "context"

// The per-person rules of the Dashboard's People actions (V4-16): who may
// change whose account, and to which role. Every action on another
// person's account (grant or revoke a role, end sessions, disable, enable)
// passes through allowedChange inside its transaction, so a route cannot
// skip the check by calling the service another way.

// allowedChange reports whether actor may change the account of
// targetEmail, whose role is current ("" when it has no account yet), to
// next (current itself for an action that keeps the role). Owners pass
// here; their own guards (the configured owner, the last owner, disabling
// oneself) stay with each action. An admin may act only on students and
// organisers, never on themself or the configured owner, and may only give
// the student or organiser role. Anyone below admin may do nothing.
func allowedChange(actor Actor, ownerEmail, targetEmail string, current, next Role) error {
	switch {
	case actor.Role == Owner:
		return nil
	case actor.Role != Admin:
		return forbidden("Only admins and owners can change people's accounts.")
	case canonical(targetEmail) == canonical(actor.Email):
		return forbidden("You cannot change your own account. Ask an owner.")
	case canonical(targetEmail) == canonical(ownerEmail) || !studentOrOrganiser(current):
		return forbidden("Only an owner can change an admin's or an owner's account.")
	case !studentOrOrganiser(next):
		return forbidden("Only an owner can make someone an admin or an owner.")
	}
	return nil
}

func studentOrOrganiser(r Role) bool { return r == "" || r == Student || r == Organiser }

// changeAllowed applies allowedChange for the actor on ctx. A change with
// no actor is refused: every signed-in request carries one (httpapi's
// requireRole), so a missing actor is a bug, not the server acting.
func (s *Service) changeAllowed(ctx context.Context, targetEmail string, current, next Role) error {
	a, ok := ActorFrom(ctx)
	if !ok {
		return forbidden("Sign in again to change people's accounts.")
	}
	return allowedChange(a, s.OwnerEmail, targetEmail, current, next)
}
