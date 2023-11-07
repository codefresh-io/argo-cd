package application

import (
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/watch"
)

type channelPayload struct {
	Application appv1.Application
	Type        watch.EventType
}

type ApplicationEventChannelSelector interface {
	Subscribe(application appv1.Application, eventType watch.EventType, callback func(application channelPayload))
}

type channelPerApplicationChannelSelector struct {
	channels map[string]chan channelPayload
}

func NewChannelPerApplicationChannelSelector() ApplicationEventChannelSelector {
	return &channelPerApplicationChannelSelector{
		channels: map[string]chan channelPayload{},
	}
}

func (s *channelPerApplicationChannelSelector) Subscribe(application appv1.Application, eventType watch.EventType, callback func(application channelPayload)) {
	log.Infof("Subscribing to application %s", application.Name)
	if s.channels[application.Name] == nil {
		s.channels[application.Name] = make(chan channelPayload, 1000)
		go func(channel chan channelPayload) {
			for {
				select {
				case app := <-channel:
					log.Infof("Received event for application %s", app.Application.Name)
					callback(app)
				}
			}
		}(s.channels[application.Name])
	}
	
	go func() {
		log.Infof("Publish to application %s", application.Name)
		s.channels[application.Name] <- channelPayload{
			Application: application,
			Type:        eventType,
		}
	}()
}
