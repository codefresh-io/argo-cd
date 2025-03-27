package cache

import (
	"fmt"
	"time"

	appv1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

func (c *Cache) SetLastApplicationEvent(a *appv1.Application, exp time.Duration) error {
	return c.cache.SetItem(lastApplicationEventKey(a), a, exp, false)
}

func (c *Cache) GetLastApplicationEvent(a *appv1.Application) (*appv1.Application, error) {
	cachedApp := appv1.Application{}
	return &cachedApp, c.cache.GetItem(lastApplicationEventKey(a), &cachedApp)
}

func (c *Cache) SetLastResourceEvent(a *appv1.Application, rs appv1.ResourceStatus, exp time.Duration, revision string) error {
	return c.cache.SetItem(lastResourceEventKey(a, rs, revision), rs, exp, false)
}

func (c *Cache) GetLastResourceEvent(a *appv1.Application, rs appv1.ResourceStatus, revision string) (appv1.ResourceStatus, error) {
	res := appv1.ResourceStatus{}
	return res, c.cache.GetItem(lastResourceEventKey(a, rs, revision), &res)
}

func lastApplicationEventKey(a *appv1.Application) string {
	return fmt.Sprintf("app|%s/%s|last-sent-event", a.Namespace, a.Name)
}

func lastResourceEventKey(a *appv1.Application, rs appv1.ResourceStatus, revision string) string {
	return fmt.Sprintf("app|%s/%s|%s|res|%s/%s/%s/%s/%s|last-sent-event",
		a.Namespace, a.Name, revision, rs.Group, rs.Version, rs.Kind, rs.Name, rs.Namespace)
}
