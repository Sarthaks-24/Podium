package ai

import (
	"errors"
	"testing"
)

func bad(answer string, reasons ...string) error {
	return &Error{Code: CodeInvalidAnswer, Message: "The model's answer could not be used.", Reasons: reasons, answer: answer}
}

func TestRepairPassesAGoodFirstAnswerThrough(t *testing.T) {
	calls := 0
	got, err := Repair(func(r Retry) (string, error) {
		calls++
		if r.Previous != "" || r.Fix != nil || r.Shorter {
			t.Errorf("the first call carries a retry: %+v", r)
		}
		return "ok", nil
	})
	if got != "ok" || err != nil || calls != 1 {
		t.Errorf("got %q %v after %d calls", got, err, calls)
	}
}

func TestRepairRetriesOnceWithThePreviousAnswerAndTheReasons(t *testing.T) {
	var seen []Retry
	got, err := Repair(func(r Retry) (string, error) {
		seen = append(seen, r)
		if len(seen) == 1 {
			return "", bad("first answer", "reason one", "reason two")
		}
		return "fixed", nil
	})
	if got != "fixed" || err != nil || len(seen) != 2 {
		t.Fatalf("got %q %v after %d calls", got, err, len(seen))
	}
	if r := seen[1]; r.Previous != "first answer" || len(r.Fix) != 2 || r.Fix[0] != "reason one" || r.Shorter {
		t.Errorf("retry %+v", r)
	}
}

func TestRepairAsksForAShorterAnswerAfterACutOff(t *testing.T) {
	var seen []Retry
	_, err := Repair(func(r Retry) (string, error) {
		seen = append(seen, r)
		if len(seen) == 1 {
			return "", &Error{Code: CodeCutOff, Message: "The answer was cut off before it was complete."}
		}
		return "short", nil
	})
	if err != nil || len(seen) != 2 || !seen[1].Shorter || seen[1].Previous != "" || seen[1].Fix != nil {
		t.Errorf("%v %+v", err, seen)
	}
}

func TestRepairStopsAfterTwoBadAnswersWithoutTheAnswerText(t *testing.T) {
	calls := 0
	_, err := Repair(func(Retry) (string, error) {
		calls++
		return "", bad("secret answer text", "still wrong")
	})
	var e *Error
	if !errors.As(err, &e) || calls != 2 || e.Code != CodeInvalidAnswer || e.answer != "" || len(e.Reasons) != 1 {
		t.Errorf("%v after %d calls", err, calls)
	}
}

func TestRepairDoesNotRetryAProviderFailureOrAPolicyRefusal(t *testing.T) {
	for name, fail := range map[string]error{
		"provider": &Error{Code: CodeRefused, Message: "The model declined this request."},
		"policy":   errors.New("a refusal of policy"),
	} {
		calls := 0
		_, err := Repair(func(Retry) (string, error) { calls++; return "", fail })
		if err != fail || calls != 1 {
			t.Errorf("%s: %v after %d calls", name, err, calls)
		}
	}
}
