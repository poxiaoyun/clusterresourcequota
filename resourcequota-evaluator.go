package clusterresourcequota

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	quota "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/apiserver/pkg/quota/v1/generic"
	"k8s.io/client-go/informers"
	"k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
)

const ResourceQuotaScopeNodeSelector corev1.ResourceQuotaScope = "NodeSelector"

var _ quota.Evaluator = &ConditionalPodEvaluator{}

func NewConditionalPodEvaluator(informers informers.SharedInformerFactory) quota.Evaluator {
	// listerFuncForResource only used when [quota.Evaluator.UsageStats] called.
	// it's ok to pass nil if UsageStats is not used.
	var listerFuncForResource quota.ListerForResourceFunc
	if informers != nil {
		listerFuncForResource = generic.ListerFuncForResourceFunc(informers.ForResource)
	}
	return &ConditionalPodEvaluator{
		Evaluator:           core.NewPodEvaluator(listerFuncForResource, clock.RealClock{}),
		listFuncByNamespace: generic.ListResourceUsingListerFunc(listerFuncForResource, corev1.SchemeGroupVersion.WithResource("pods")),
	}
}

type ConditionalPodEvaluator struct {
	Evaluator           quota.Evaluator
	listFuncByNamespace generic.ListFuncByNamespace
}

// Constraints implements v1.Evaluator.
func (c *ConditionalPodEvaluator) Constraints(required []corev1.ResourceName, item runtime.Object) error {
	return c.Evaluator.Constraints(required, item)
}

// GroupResource implements v1.Evaluator.
func (c *ConditionalPodEvaluator) GroupResource() schema.GroupResource {
	return c.Evaluator.GroupResource()
}

// Handles implements v1.Evaluator.
func (c *ConditionalPodEvaluator) Handles(operation admission.Attributes) bool {
	return c.Evaluator.Handles(operation)
}

// MatchingResources implements v1.Evaluator.
func (c *ConditionalPodEvaluator) MatchingResources(input []corev1.ResourceName) []corev1.ResourceName {
	return c.Evaluator.MatchingResources(input)
}

// MatchingScopes implements v1.Evaluator.
func (c *ConditionalPodEvaluator) MatchingScopes(item runtime.Object, scopes []corev1.ScopedResourceSelectorRequirement) ([]corev1.ScopedResourceSelectorRequirement, error) {
	matchedScopes, err := c.Evaluator.MatchingScopes(item, scopes)
	if err != nil {
		return nil, err
	}
	// add this evaluator specific scopes
	for _, selector := range scopes {
		match, err := ConditionalPodMatchesScopeFunc(selector, item)
		if err != nil {
			return []corev1.ScopedResourceSelectorRequirement{}, fmt.Errorf("error on matching scope %v: %v", selector, err)
		}
		if match {
			matchedScopes = append(matchedScopes, selector)
		}
	}
	return matchedScopes, nil
}

// UncoveredQuotaScopes implements v1.Evaluator.
func (c *ConditionalPodEvaluator) UncoveredQuotaScopes(limitedScopes []corev1.ScopedResourceSelectorRequirement, matchedQuotaScopes []corev1.ScopedResourceSelectorRequirement) ([]corev1.ScopedResourceSelectorRequirement, error) {
	return c.Evaluator.UncoveredQuotaScopes(limitedScopes, matchedQuotaScopes)
}

// Usage implements v1.Evaluator.
func (c *ConditionalPodEvaluator) Usage(item runtime.Object) (corev1.ResourceList, error) {
	return c.Evaluator.Usage(item)
}

// Matches implements v1.Evaluator.
func (c *ConditionalPodEvaluator) Matches(resourceQuota *corev1.ResourceQuota, item runtime.Object) (bool, error) {
	ok, err := generic.Matches(resourceQuota, item, c.Evaluator.MatchingResources, ConditionalPodMatchesScopeFunc)
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	// fall back to underlying evaluator
	return c.Evaluator.Matches(resourceQuota, item)
}

// UsageStats calculates aggregate usage for the object.
func (c *ConditionalPodEvaluator) UsageStats(options quota.UsageStatsOptions) (quota.UsageStats, error) {
	usage, err := generic.CalculateUsageStats(options, c.listFuncByNamespace, ConditionalPodMatchesScopeFunc, c.Usage)
	if err != nil {
		return quota.UsageStats{}, err
	}
	// If we have any usage from the generic calculation, return it.  Otherwise, fall back to the underlying evaluator.
	if len(usage.Used) != 0 {
		return usage, nil
	}
	return c.Evaluator.UsageStats(options)
}

func ConditionalPodMatchesScopeFunc(selector corev1.ScopedResourceSelectorRequirement, object runtime.Object) (bool, error) {
	pod, ok := object.(*corev1.Pod)
	if !ok {
		return false, nil
	}
	if selector.ScopeName == ResourceQuotaScopeNodeSelector {
		return PodNodeSelectorMatch(pod, selector)
	}
	return false, nil
}

func PodNodeSelectorMatch(pod *corev1.Pod, selector corev1.ScopedResourceSelectorRequirement) (bool, error) {
	podNodeSelector := pod.Spec.NodeSelector
	switch selector.Operator {
	case corev1.ScopeSelectorOpIn:
		for _, value := range selector.Values {
			selector, err := labels.Parse(value)
			if err != nil {
				return false, err
			}
			if selector.Matches(labels.Set(podNodeSelector)) {
				return true, nil
			}
		}
		return false, nil
	case corev1.ScopeSelectorOpNotIn:
		for _, value := range selector.Values {
			selector, err := labels.Parse(value)
			if err != nil {
				return false, err
			}
			if selector.Matches(labels.Set(podNodeSelector)) {
				return false, nil
			}
		}
		return true, nil
	case corev1.ScopeSelectorOpExists:
		return len(podNodeSelector) != 0, nil
	case corev1.ScopeSelectorOpDoesNotExist:
		return len(podNodeSelector) == 0, nil
	default:
		return false, fmt.Errorf("unsupported operator %v for NodeSelector scope", selector.Operator)
	}
}
