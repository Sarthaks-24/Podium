package ai

import "errors"

// Retry is what the one repeat call carries: the previous answer and the
// reasons it was refused, or Shorter after an answer was cut off.
type Retry struct {
	Previous string
	Fix      []string
	Shorter  bool
}

// Repair runs try, and runs it once more when the first answer was unusable:
// try returns an *Error with code invalid_answer (carrying the answer and the
// reasons, the way a parser refusal is built) or cut_off. The second call gets
// the reasons and the previous answer, or Shorter after a cut-off. Any other
// error, a refusal of policy or a provider failure, ends the run at once. The
// error after two bad answers is the second one, without the answer text.
func Repair[T any](try func(Retry) (T, error)) (T, error) {
	var zero T
	var last *Error
	var retry Retry
	for attempt := 0; attempt < 2; attempt++ {
		out, err := try(retry)
		if err == nil {
			return out, nil
		}
		var e *Error
		if !errors.As(err, &e) || (e.Code != CodeInvalidAnswer && e.Code != CodeCutOff) {
			return zero, err
		}
		last = e
		if e.Code == CodeCutOff {
			retry = Retry{Shorter: true}
		} else {
			retry = Retry{Previous: e.answer, Fix: e.Reasons}
		}
	}
	return zero, &Error{Code: last.Code, Message: last.Message, Reasons: last.Reasons}
}
