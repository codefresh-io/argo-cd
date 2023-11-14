package application

import (
	"context"
	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/v2/server/metrics"
	"github.com/argoproj/argo-cd/v2/util/settings"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
	"k8s.io/apimachinery/pkg/watch"
	"time"
)

type channelPayload struct {
	Application appv1.Application
	Type        watch.EventType
}

type ApplicationEventChannelSelector interface {
	Subscribe(application appv1.Application, eventType watch.EventType, callback func(application channelPayload) bool)
}

type channelPerApplicationChannelSelector struct {
	channels      map[string]chan channelPayload
	sem           *semaphore.Weighted
	metricsServer *metrics.MetricsServer
}

func NewChannelPerApplicationChannelSelector(metricsServer *metrics.MetricsServer, settingsManager *settings.SettingsManager) ApplicationEventChannelSelector {
	var sem = semaphore.NewWeighted(int64(settingsManager.GetAmountOfThreads()))
	selector := &channelPerApplicationChannelSelector{
		channels:      map[string]chan channelPayload{},
		sem:           sem,
		metricsServer: metricsServer,
	}
	go selector.printChannelSizes()
	return selector
}

func (s *channelPerApplicationChannelSelector) printChannelSizes() {
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				for k, v := range s.channels {
					s.metricsServer.SetChannelSizeCounter(k, float64(len(v)))
					log.Infof("Channel %s has size %d", k, len(v))
				}
			}
		}
	}()
}

func (s *channelPerApplicationChannelSelector) Subscribe(application appv1.Application, eventType watch.EventType, callback func(application channelPayload) bool) {
	hash := application.Name

	if s.channels[hash] == nil {
		s.channels[hash] = make(chan channelPayload, 100)
		go func(channel chan channelPayload) {
			for {
				select {
				case app := <-channel:
					_ = s.sem.Acquire(context.Background(), 1)
					s.metricsServer.IncLocksCounter()
					result := callback(app)
					log.Printf("Callback result is %v, channel is %s, size is %d", result, hash, len(channel))
					s.sem.Release(1)
					s.metricsServer.DecLocksCounter()
				}
			}
		}(s.channels[hash])
	}

	go func() {
		select {
		case s.channels[hash] <- channelPayload{
			Application: application,
			Type:        eventType,
		}:
		default:
			s.metricsServer.IncAmountOfIgnoredEventsCounter(application.Name)
		}

		//log.Infof("Application channel #%d has size %d", hash, len(s.channels[hash]))
	}()
}
