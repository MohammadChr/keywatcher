package notify

import (
	"context"
	"fmt"
	"strings"
)

type MultiNotifier struct {
	notifiers []Notifier
}

func NewMulti(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

func (m *MultiNotifier) Send(ctx context.Context, msg Message) error {
	var errs []string
	for _, n := range m.notifiers {
		if err := n.Send(ctx, msg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", n.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("notify.MultiNotifier errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *MultiNotifier) Names() []string {
	var names []string
	for _, n := range m.notifiers {
		names = append(names, n.Name())
	}
	return names
}
