package cache

import (
	"fmt"

	"github.com/argoproj/argo-cd/v2/pkg/codefresh"
	cacheutil "github.com/argoproj/argo-cd/v2/util/cache"
)

func CfAppConfigCacheKey(namespace, name string) string {
	return fmt.Sprintf("cf_app_config:%s:%s", namespace, name)
}

func (c *Cache) GetCfAppConfig(namespace, name string) (*codefresh.PromotionTemplate, error) {
	item := &codefresh.PromotionTemplate{}
	return item, c.cache.GetItem(CfAppConfigCacheKey(namespace, name), item)
}

func (c *Cache) SetCfAppConfig(namespace, name string, item *codefresh.PromotionTemplate) error {
	return c.cache.SetItem(CfAppConfigCacheKey(namespace, name), item, &cacheutil.CacheActionOpts{Expiration: c.cfAppConfigCacheExpiration, Delete: false})
}
