package application

import (
	"sync"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/watch"

	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

type subscriber struct {
	ch      chan *appv1.ApplicationWatchEvent
	filters []func(*appv1.ApplicationWatchEvent) bool
}

func (s *subscriber) matches(event *appv1.ApplicationWatchEvent) bool {
	for i := range s.filters {
		if !s.filters[i](event) {
			return false
		}
	}
	return true
}

// Broadcaster is an interface for broadcasting application informer watch events to multiple subscribers.
type Broadcaster interface {
	Subscribe(ch chan *appv1.ApplicationWatchEvent, filters ...func(event *appv1.ApplicationWatchEvent) bool) func()
	SubscribeOnAdd(ch chan *appv1.ApplicationWatchEvent, filters ...func(event *appv1.ApplicationWatchEvent) bool) func()
	SubscribeOnUpdate(ch chan *appv1.ApplicationWatchEvent, filters ...func(event *appv1.ApplicationWatchEvent) bool) func()
	SubscribeOnDelete(ch chan *appv1.ApplicationWatchEvent, filters ...func(event *appv1.ApplicationWatchEvent) bool) func()

	OnAdd(interface{})
	OnUpdate(interface{}, interface{})
	OnDelete(interface{})
}

type broadcasterHandler struct {
	lock        sync.Mutex
	subscribers []*subscriber

	onAddLock        sync.Mutex
	onAddSubscribers []*subscriber

	onUpdateLock        sync.Mutex
	onUpdateSubscribers []*subscriber

	onDeleteLock        sync.Mutex
	onDeleteSubscribers []*subscriber
}

func (b *broadcasterHandler) notify(event *appv1.ApplicationWatchEvent) {
	// Make a local copy of b.subscribers, then send channel events outside the lock,
	// to avoid data race on b.subscribers changes
	subscribers := []*subscriber{}
	b.lock.Lock()
	subscribers = append(subscribers, b.subscribers...)
	b.lock.Unlock()

	for _, s := range subscribers {
		if s.matches(event) {
			select {
			case s.ch <- event:
			default:
				// drop event if cannot send right away
				log.WithField("application", event.Application.Name).Warn("unable to send event notification")
			}
		}
	}
}

func (b *broadcasterHandler) notifyOnUpdate(event *appv1.ApplicationWatchEvent) {
	subscribers := []*subscriber{}
	b.onUpdateLock.Lock()
	subscribers = append(subscribers, b.onUpdateSubscribers...)
	b.onUpdateLock.Unlock()

	for _, s := range subscribers {
		if s.matches(event) {
			select {
			case s.ch <- event:
			default:
				// drop event if cannot send right away
				log.WithField("application", event.Application.Name).Warn("unable to send onUpdate event notification")
			}
		}
	}
}

func (b *broadcasterHandler) notifyOnDelete(event *appv1.ApplicationWatchEvent) {
	subscribers := []*subscriber{}
	b.onDeleteLock.Lock()
	subscribers = append(subscribers, b.onDeleteSubscribers...)
	b.onDeleteLock.Unlock()

	for _, s := range subscribers {
		if s.matches(event) {
			select {
			case s.ch <- event:
			default:
				// drop event if cannot send right away
				log.WithField("application", event.Application.Name).Warn("unable to send onDelete event notification")
			}
		}
	}
}

func (b *broadcasterHandler) notifyOnAdd(event *appv1.ApplicationWatchEvent) {
	subscribers := []*subscriber{}
	b.onAddLock.Lock()
	subscribers = append(subscribers, b.onAddSubscribers...)
	b.onAddLock.Unlock()

	for _, s := range subscribers {
		if s.matches(event) {
			select {
			case s.ch <- event:
			default:
				// drop event if cannot send right away
				log.WithField("application", event.Application.Name).Warn("unable to send onAdd event notification")
			}
		}
	}
}

// Subscribe forward application informer watch events to the provided channel.
// The watch events are dropped if no receives are reading events from the channel so the channel must have
// buffer if dropping events is not acceptable.
func (b *broadcasterHandler) Subscribe(ch chan *appv1.ApplicationWatchEvent, filters ...func(event *appv1.ApplicationWatchEvent) bool) func() {
	b.lock.Lock()
	defer b.lock.Unlock()
	subscriber := &subscriber{ch, filters}
	b.subscribers = append(b.subscribers, subscriber)
	return func() {
		b.lock.Lock()
		defer b.lock.Unlock()
		for i := range b.subscribers {
			if b.subscribers[i] == subscriber {
				b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
				break
			}
		}
	}
}

func (b *broadcasterHandler) SubscribeOnAdd(ch chan *appv1.ApplicationWatchEvent, filters ...func(event *appv1.ApplicationWatchEvent) bool) func() {
	b.onAddLock.Lock()
	defer b.onAddLock.Unlock()
	subscriber := &subscriber{ch, filters}
	b.onAddSubscribers = append(b.onAddSubscribers, subscriber)
	return func() {
		b.lock.Lock()
		defer b.lock.Unlock()
		for i := range b.onAddSubscribers {
			if b.onAddSubscribers[i] == subscriber {
				b.onAddSubscribers = append(b.onAddSubscribers[:i], b.onAddSubscribers[i+1:]...)
				break
			}
		}
	}
}

func (b *broadcasterHandler) SubscribeOnUpdate(ch chan *appv1.ApplicationWatchEvent, filters ...func(event *appv1.ApplicationWatchEvent) bool) func() {
	b.onUpdateLock.Lock()
	defer b.onUpdateLock.Unlock()
	subscriber := &subscriber{ch, filters}
	b.onUpdateSubscribers = append(b.onUpdateSubscribers, subscriber)
	return func() {
		b.lock.Lock()
		defer b.lock.Unlock()
		for i := range b.onUpdateSubscribers {
			if b.onUpdateSubscribers[i] == subscriber {
				b.onUpdateSubscribers = append(b.onUpdateSubscribers[:i], b.onUpdateSubscribers[i+1:]...)
				break
			}
		}
	}
}

func (b *broadcasterHandler) SubscribeOnDelete(ch chan *appv1.ApplicationWatchEvent, filters ...func(event *appv1.ApplicationWatchEvent) bool) func() {
	b.onDeleteLock.Lock()
	defer b.onDeleteLock.Unlock()
	subscriber := &subscriber{ch, filters}
	b.onDeleteSubscribers = append(b.onDeleteSubscribers, subscriber)
	return func() {
		b.lock.Lock()
		defer b.lock.Unlock()
		for i := range b.onDeleteSubscribers {
			if b.onDeleteSubscribers[i] == subscriber {
				b.onDeleteSubscribers = append(b.onDeleteSubscribers[:i], b.onDeleteSubscribers[i+1:]...)
				break
			}
		}
	}
}

func (b *broadcasterHandler) OnAdd(obj interface{}) {
	if app, ok := obj.(*appv1.Application); ok {
		b.notify(&appv1.ApplicationWatchEvent{Application: *app, Type: watch.Added})
		b.notifyOnAdd(&appv1.ApplicationWatchEvent{Application: *app, Type: watch.Added})
	}
}

func (b *broadcasterHandler) OnUpdate(_, newObj interface{}) {
	if app, ok := newObj.(*appv1.Application); ok {
		b.notify(&appv1.ApplicationWatchEvent{Application: *app, Type: watch.Modified})
		b.notifyOnUpdate(&appv1.ApplicationWatchEvent{Application: *app, Type: watch.Modified})
	}
}

func (b *broadcasterHandler) OnDelete(obj interface{}) {
	if app, ok := obj.(*appv1.Application); ok {
		b.notify(&appv1.ApplicationWatchEvent{Application: *app, Type: watch.Deleted})
		b.notifyOnDelete(&appv1.ApplicationWatchEvent{Application: *app, Type: watch.Deleted})
	}
}
