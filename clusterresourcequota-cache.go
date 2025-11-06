package clusterresourcequota

import (
	"context"
	"maps"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	quotav1 "xiaoshiai.cn/clusterresourcequota/apis/quota/v1"
	"xiaoshiai.cn/common/log"
)

type CacheSyner struct {
	Cache    *ResourceQuotaCache
	Client   client.Client
	Interval time.Duration
}

var _ manager.Runnable = &CacheSyner{}

func (c *CacheSyner) Start(ctx context.Context) error {
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := c.SyncOnce(ctx); err != nil {
				log.FromContext(ctx).Error(err, "Sync ResourceQuotaCache")
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (c *CacheSyner) SyncOnce(ctx context.Context) error {
	rqlist := &quotav1.ResourceQuotaList{}
	if err := c.Client.List(ctx, rqlist); err != nil {
		return err
	}
	c.Cache.Sync(rqlist.Items)
	return nil
}

func NewResourceQuotaCache() *ResourceQuotaCache {
	return &ResourceQuotaCache{
		quotacache: map[string]*ClusterResourceQuotaCache{},
	}
}

type ResourceQuotaCache struct {
	lock sync.RWMutex
	// clusterquotaname ->  usage
	quotacache map[string]*ClusterResourceQuotaCache
}

// Sync syncs the cache with the given list of ResourceQuotas
// it will add/update/remove the cache entries as needed
// Due to the possibility of status updates from both the admission controller and the kube-controller-manager
// it only updates the cache entries if the last update time is older than 10 seconds
func (r *ResourceQuotaCache) Sync(quotas []quotav1.ResourceQuota) {
	r.lock.Lock()
	defer r.lock.Unlock()

	now := time.Now()
	grouped := map[string]map[string]*ResourceUsageInfo{}
	for _, rq := range quotas {
		if rq.Labels == nil {
			continue
		}
		crqname := rq.Labels[LabelClusterResourceQuota]
		if crqname == "" {
			continue
		}
		if _, ok := grouped[crqname]; !ok {
			grouped[crqname] = map[string]*ResourceUsageInfo{}
		}
		grouped[crqname][rq.Namespace] = &ResourceUsageInfo{Hard: rq.Status.Hard, Used: rq.Status.Used, LastUpdate: now}
	}
	for existingClusterRQName, crqcache := range r.quotacache {
		updatedQuotas, ok := grouped[existingClusterRQName]
		if !ok {
			// remove clusterresourcequota cache that no longer exists
			delete(r.quotacache, existingClusterRQName)
			continue
		}
		// merge quotas
		for existingNamespace, existingUsage := range crqcache.Quotas {
			updatedUsage, ok := updatedQuotas[existingNamespace]
			if !ok {
				// remove namespace quota that no longer exists
				delete(crqcache.Quotas, existingNamespace)
				continue
			}
			// skip update if last update is within 10 seconds
			// it means the usage is recently updated from admission
			// it's the most up-to-date usage info
			if existingUsage.LastUpdate.Add(10 * time.Second).After(time.Now()) {
				continue
			}
			// update existing usage info
			*existingUsage = *updatedUsage
			delete(updatedQuotas, existingNamespace)
		}
		// add new namespace quotas
		maps.Copy(crqcache.Quotas, updatedQuotas)
		delete(grouped, existingClusterRQName)
	}
	for newClusterRQName, quotas := range grouped {
		crqcache := &ClusterResourceQuotaCache{Quotas: map[string]*ResourceUsageInfo{}}
		r.quotacache[newClusterRQName] = crqcache
		// add new namespace quotas
		maps.Copy(crqcache.Quotas, quotas)
	}
}

// OnClusterResourceQuota lock and execute function fn on the quota usage map for the given clusterresourcequota name
func (c *ResourceQuotaCache) GetOrCreate(ctx context.Context, name string) *ClusterResourceQuotaCache {
	c.lock.Lock()
	defer c.lock.Unlock()
	val, ok := c.quotacache[name]
	if !ok {
		val = &ClusterResourceQuotaCache{Quotas: map[string]*ResourceUsageInfo{}}
		c.quotacache[name] = val
	}
	return val
}

type ClusterResourceQuotaCache struct {
	Lock   sync.RWMutex
	Quotas map[string]*ResourceUsageInfo
}

func (c *ClusterResourceQuotaCache) OnLock(ctx context.Context, fn func(cache *ClusterResourceQuotaCache) error) error {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	return fn(c)
}

type ResourceUsageInfo struct {
	Hard corev1.ResourceList
	Used corev1.ResourceList
	// LastUpdate record the last update time of the usage info
	LastUpdate time.Time
}
