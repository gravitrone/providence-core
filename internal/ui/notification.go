package ui

import (
	"strings"
	"time"
)

// NotificationDismissMS is how long a notification stays visible before auto-dismissing.
const NotificationDismissMS = 5000

// MaxNotifications is the maximum number of concurrent notifications.
const MaxNotifications = 3

// Notification is a single toast-style notification.
type Notification struct {
	Text      string
	CreatedAt time.Time
	Dismissed bool
}

// NotificationModel manages a list of time-limited notifications.
type NotificationModel struct {
	Items []Notification
}

// Add appends a notification, dropping the oldest if at capacity.
func (n *NotificationModel) Add(text string) {
	if len(n.Items) >= MaxNotifications {
		n.Items = n.Items[1:]
	}
	n.Items = append(n.Items, Notification{
		Text:      text,
		CreatedAt: time.Now(),
	})
}

// Tick prunes expired and dismissed notifications.
func (n *NotificationModel) Tick() {
	now := time.Now()
	active := n.Items[:0]
	for _, item := range n.Items {
		if !item.Dismissed && now.Sub(item.CreatedAt) < time.Duration(NotificationDismissMS)*time.Millisecond {
			active = append(active, item)
		}
	}
	n.Items = active
}

// View renders visible notifications as muted flame-styled lines.
func (n *NotificationModel) View(_ int) string {
	if len(n.Items) == 0 {
		return ""
	}
	var lines []string
	for _, item := range n.Items {
		lines = append(lines, "  "+item.Text)
	}
	return strings.Join(lines, "\n")
}
