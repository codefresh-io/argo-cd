package application

import (
	"context"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
	"k8s.io/apimachinery/pkg/watch"
)

type channelPayload struct {
	Application appv1.Application
	Type        watch.EventType
}

type ApplicationEventChannelSelector interface {
	Subscribe(application appv1.Application, eventType watch.EventType, callback func(application channelPayload) bool)
}

type channelPerApplicationChannelSelector struct {
	channels map[string]chan channelPayload
	sem      *semaphore.Weighted
}

func NewChannelPerApplicationChannelSelector() ApplicationEventChannelSelector {
	var sem = semaphore.NewWeighted(int64(20))
	return &channelPerApplicationChannelSelector{
		channels: map[string]chan channelPayload{},
		sem:      sem,
	}
}

func hash(data string) int {
	sum := 0
	for i := 0; i < len(data); i++ {
		sum = sum + int(data[i])
	}
	return sum % 24
}

func (s *channelPerApplicationChannelSelector) Subscribe(application appv1.Application, eventType watch.EventType, callback func(application channelPayload) bool) {
	hash := application.Name

	if s.channels[hash] == nil {
		s.channels[hash] = make(chan channelPayload, 5000)
		go func(channel chan channelPayload) {
			for {
				select {
				case app := <-channel:
					_ = s.sem.Acquire(context.Background(), 1)
					result := callback(app)
					log.Printf("Callback result is %v, channel is %s, size is %d", result, hash, len(channel))
					s.sem.Release(1)
				}
			}
		}(s.channels[hash])
	}

	go func() {
		s.channels[hash] <- channelPayload{
			Application: application,
			Type:        eventType,
		}
		//log.Infof("Application channel #%d has size %d", hash, len(s.channels[hash]))
	}()
}
