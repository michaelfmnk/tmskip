// Package notify posts macOS notifications.
package notify

import (
	"fmt"
	"strings"

	"github.com/michaelfmnk/tmskip/internal/runner"
)

// Notifier posts one notification.
type Notifier interface {
	Notify(title, body string) error
}

// Osascript posts through /usr/bin/osascript. Title and body are passed as
// script arguments, never interpolated into the script, so no quoting bugs.
// (Notifications show as coming from Script Editor.)
type Osascript struct{ R runner.Runner }

func (o Osascript) Notify(title, body string) error {
	_, stderr, code, err := o.R.Run("/usr/bin/osascript",
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		title, body)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("osascript: exit %d: %s", code, strings.TrimSpace(string(stderr)))
	}
	return nil
}

// Message is one notification sent to a Fake.
type Message struct{ Title, Body string }

// Fake records notifications.
type Fake struct{ Sent []Message }

func (f *Fake) Notify(title, body string) error {
	f.Sent = append(f.Sent, Message{title, body})
	return nil
}
