package permissions

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDenialTrackerRecordsAndReturnsHistory(t *testing.T) {
	dt := NewDenialTracker()
	dt.Record("Bash", map[string]interface{}{"command": "rm -rf /"})
	dt.Record("Write", map[string]interface{}{"file_path": "/etc/passwd"})

	history := dt.History()
	require.Len(t, history, 2)
	// Most recent first.
	assert.Equal(t, "Write", history[0].Tool)
	assert.Equal(t, "/etc/passwd", history[0].Input)
	assert.Equal(t, "Bash", history[1].Tool)
}

func TestDenialTrackerCoalescesRepeatedEntries(t *testing.T) {
	dt := NewDenialTracker()
	dt.Record("Bash", map[string]interface{}{"command": "rm file"})
	dt.Record("Bash", map[string]interface{}{"command": "rm file"})
	dt.Record("Bash", map[string]interface{}{"command": "rm file"})

	history := dt.History()
	require.Len(t, history, 1)
	assert.Equal(t, 3, history[0].Count)
}

func TestDenialTrackerCapAt100(t *testing.T) {
	dt := NewDenialTracker()
	for i := 0; i < 150; i++ {
		dt.Record("Bash", map[string]interface{}{"command": fmt.Sprintf("cmd-%d", i)})
	}

	require.Equal(t, 100, dt.Len())
	history := dt.History()
	// The first 50 inputs should have been evicted (cmd-0 through cmd-49).
	for _, rec := range history {
		assert.NotContains(t, rec.Input, "cmd-0 ")
	}
	// Most recent should be the last inserted.
	assert.Equal(t, "cmd-149", history[0].Input)
}

func TestDenialTrackerUpdatesTimestampOnRecord(t *testing.T) {
	dt := NewDenialTracker()
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)

	calls := 0
	dt.nowFunc = func() time.Time {
		calls++
		if calls == 1 {
			return t0
		}
		return t1
	}

	dt.Record("Bash", map[string]interface{}{"command": "same"})
	dt.Record("Bash", map[string]interface{}{"command": "same"})

	history := dt.History()
	require.Len(t, history, 1)
	assert.Equal(t, t1, history[0].Timestamp)
}

func TestDenialTrackerConcurrentSafe(t *testing.T) {
	dt := NewDenialTracker()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dt.Record("Bash", map[string]interface{}{"command": fmt.Sprintf("go-%d", i)})
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 50, dt.Len())
}
