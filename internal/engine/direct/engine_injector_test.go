package direct

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func (f *fakeInj) MarkAttached()                    {}
func (f *fakeInj) RingChangedSinceLastAttach() bool { return false }
func (f *fakeInj) ScreenshotPNGs() [][]byte         { return nil }
func (f *fakeInj) Transcript() string               { return "" }

// configurableInj is a richer fakeInj used to validate the ambient-frame
// attach contract exercised inside DirectEngine.Send:
//
//	if inj.RingChangedSinceLastAttach() {
//	    pngs := inj.ScreenshotPNGs()
//	    if len(pngs) > 0 {
//	        attach selectAmbientFrames(pngs)
//	        inj.MarkAttached()
//	    }
//	}
//
// Because Send() spawns a full agent loop and requires a live provider, the
// contract is re-asserted here against the pure helpers used by Send - this
// lets us cover the three behavior branches without booting a real engine.
type configurableInj struct {
	pngs          [][]byte
	transcript    string
	ringChanged   bool
	markedCount   int
	ringCalled    int
	pngsCalled    int
	transcriptGet int
}

func (c *configurableInj) PendingSystemReminder() string { return "" }
func (c *configurableInj) MarkAttached()                 { c.markedCount++ }
func (c *configurableInj) RingChangedSinceLastAttach() bool {
	c.ringCalled++
	return c.ringChanged
}
func (c *configurableInj) ScreenshotPNGs() [][]byte {
	c.pngsCalled++
	return c.pngs
}
func (c *configurableInj) Transcript() string {
	c.transcriptGet++
	return c.transcript
}

// simulateAttach replays the Send()-side attach logic against the injector,
// returning the ImageData slice that Send would append to pendingImages. This
// is intentionally a mirror of the branch in engine.go around line 708 so
// test coverage sits on the observable contract, not the goroutine-backed
// Send path (which needs a real provider and thus cannot be unit-tested).
func simulateAttach(inj contextInjector) []ImageData {
	var images []ImageData
	if inj == nil {
		return images
	}
	if !inj.RingChangedSinceLastAttach() {
		return images
	}
	pngs := inj.ScreenshotPNGs()
	if len(pngs) == 0 {
		return images
	}
	for _, png := range selectAmbientFrames(pngs) {
		images = append(images, ImageData{MediaType: "image/png", Data: png})
	}
	inj.MarkAttached()
	return images
}

// TestEngineSend_AmbientAttachesSubsampledFrames verifies that a bridge with 6
// PNGs causes 3 frames (oldest + two newest) to be attached and MarkAttached
// to be called once.
func TestEngineSend_AmbientAttachesSubsampledFrames(t *testing.T) {
	pngs := [][]byte{{1}, {2}, {3}, {4}, {5}, {6}}
	inj := &configurableInj{pngs: pngs, ringChanged: true}

	images := simulateAttach(inj)

	require.Len(t, images, 3, "must attach exactly 3 frames for a ring of 6")
	assert.Equal(t, pngs[0], images[0].Data, "oldest frame at index 0")
	assert.Equal(t, pngs[4], images[1].Data, "n-2 frame at index 1")
	assert.Equal(t, pngs[5], images[2].Data, "newest frame at index 2")
	for _, img := range images {
		assert.Equal(t, "image/png", img.MediaType)
	}
	assert.Equal(t, 1, inj.markedCount, "MarkAttached must be called exactly once")
	assert.Equal(t, 1, inj.ringCalled)
	assert.Equal(t, 1, inj.pngsCalled)
}

// TestEngineSend_SkipsAttachWhenRingUnchanged verifies that when the ring is
// unchanged since last attach, no frames are pulled and MarkAttached stays
// unchanged - the idle-ember-tick optimization.
func TestEngineSend_SkipsAttachWhenRingUnchanged(t *testing.T) {
	pngs := [][]byte{{1}, {2}, {3}, {4}}
	inj := &configurableInj{pngs: pngs, ringChanged: false}

	images := simulateAttach(inj)

	assert.Empty(t, images, "no frames attached when ring unchanged")
	assert.Equal(t, 1, inj.ringCalled, "RingChangedSinceLastAttach polled once")
	assert.Equal(t, 0, inj.pngsCalled, "ScreenshotPNGs must NOT be read when ring is stable")
	assert.Equal(t, 0, inj.markedCount, "MarkAttached must not fire on a no-op turn")
}

// TestEngineSend_NoImagesWhenInjectorReturnsNil verifies safe behavior when
// the injector is present but ScreenshotPNGs returns nil/empty (overlay up
// but screen-capture disabled).
func TestEngineSend_NoImagesWhenInjectorReturnsNil(t *testing.T) {
	inj := &configurableInj{pngs: nil, ringChanged: true}

	images := simulateAttach(inj)

	assert.Empty(t, images)
	assert.Equal(t, 1, inj.pngsCalled, "ScreenshotPNGs is consulted once")
	assert.Equal(t, 0, inj.markedCount, "MarkAttached must not fire when there is nothing to attach")

	// Also verify nil injector is safe.
	var nilInj contextInjector
	assert.Empty(t, simulateAttach(nilInj))
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
