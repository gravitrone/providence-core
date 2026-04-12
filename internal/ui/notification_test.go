package ui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNotificationAdd(t *testing.T) {
	var nm NotificationModel
	nm.Add("hello")
	nm.Add("world")

	assert.Len(t, nm.Items, 2)
	assert.Equal(t, "hello", nm.Items[0].Text)
	assert.Equal(t, "world", nm.Items[1].Text)
}

func TestNotificationOverflow(t *testing.T) {
	var nm NotificationModel
	for i := 0; i < MaxNotifications+2; i++ {
		nm.Add("msg")
	}
	assert.Len(t, nm.Items, MaxNotifications, "should not exceed MaxNotifications")
}

func TestNotificationTick_Expiry(t *testing.T) {
	var nm NotificationModel
	// Inject an already-expired notification.
	nm.Items = append(nm.Items, Notification{
		Text:      "old",
		CreatedAt: time.Now().Add(-10 * time.Second),
	})
	nm.Items = append(nm.Items, Notification{
		Text:      "fresh",
		CreatedAt: time.Now(),
	})

	nm.Tick()

	assert.Len(t, nm.Items, 1)
	assert.Equal(t, "fresh", nm.Items[0].Text)
}

func TestNotificationTick_Dismissed(t *testing.T) {
	var nm NotificationModel
	nm.Items = append(nm.Items, Notification{
		Text:      "dismissed",
		CreatedAt: time.Now(),
		Dismissed: true,
	})
	nm.Items = append(nm.Items, Notification{
		Text:      "active",
		CreatedAt: time.Now(),
	})

	nm.Tick()

	assert.Len(t, nm.Items, 1)
	assert.Equal(t, "active", nm.Items[0].Text)
}

func TestNotificationView_Empty(t *testing.T) {
	var nm NotificationModel
	assert.Equal(t, "", nm.View(80))
}

func TestNotificationView_WithItems(t *testing.T) {
	var nm NotificationModel
	nm.Add("test notification")

	view := nm.View(80)
	assert.Contains(t, view, "test notification")
}
