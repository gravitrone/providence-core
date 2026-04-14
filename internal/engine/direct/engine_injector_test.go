package direct

import "testing"

// fakeInj is a stub contextInjector for testing.
type fakeInj struct {
	reminder string
	called   int
}

func (f *fakeInj) PendingSystemReminder() string {
	f.called++
	s := f.reminder
	f.reminder = "" // clear after read
	return s
}

// TestPrepareUserText_WithReminder verifies that a pending reminder is prepended.
func TestPrepareUserText_WithReminder(t *testing.T) {
	e := &DirectEngine{}
	inj := &fakeInj{reminder: "<system-reminder origin=\"overlay\">ctx</system-reminder>"}
	e.SetContextInjector(inj)

	got := e.prepareUserText("hello")
	want := "<system-reminder origin=\"overlay\">ctx</system-reminder>\n\nhello"
	if got != want {
		t.Errorf("prepareUserText got %q, want %q", got, want)
	}
	if inj.called != 1 {
		t.Errorf("PendingSystemReminder called %d times, want 1", inj.called)
	}
}

// TestPrepareUserText_ClearedOnSecondCall verifies that the reminder is only
// prepended once - the injector clears on read.
func TestPrepareUserText_ClearedOnSecondCall(t *testing.T) {
	e := &DirectEngine{}
	inj := &fakeInj{reminder: "<system-reminder origin=\"overlay\">ctx</system-reminder>"}
	e.SetContextInjector(inj)

	e.prepareUserText("first") // consumes reminder
	got := e.prepareUserText("second")
	if got != "second" {
		t.Errorf("second call: got %q, want %q", got, "second")
	}
}

// TestPrepareUserText_NilInjector verifies nil-safe path.
func TestPrepareUserText_NilInjector(t *testing.T) {
	e := &DirectEngine{}
	got := e.prepareUserText("hello")
	if got != "hello" {
		t.Errorf("nil injector: got %q, want %q", got, "hello")
	}
}

// TestPrepareUserText_EmptyReminder verifies no modification when injector
// returns an empty string.
func TestPrepareUserText_EmptyReminder(t *testing.T) {
	e := &DirectEngine{}
	e.SetContextInjector(&fakeInj{reminder: ""})

	got := e.prepareUserText("hello")
	if got != "hello" {
		t.Errorf("empty reminder: got %q, want %q", got, "hello")
	}
}
