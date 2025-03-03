// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha1

import (
	v1 "k8s.io/client-go/applyconfigurations/meta/v1"
)

// ControlPlaneApplyConfiguration represents a declarative configuration of the ControlPlane type for use
// with apply.
type ControlPlaneApplyConfiguration struct {
	Conditions   []v1.ConditionApplyConfiguration         `json:"conditions,omitempty"`
	Resource     *ResourceRefApplyConfiguration           `json:"resource,omitempty"`
	PoolResource *PoolResourceRefApplyConfiguration       `json:"poolResource,omitempty"`
	Informers    []ControlPlaneInformerApplyConfiguration `json:"informers,omitempty"`
}

// ControlPlaneApplyConfiguration constructs a declarative configuration of the ControlPlane type for use with
// apply.
func ControlPlane() *ControlPlaneApplyConfiguration {
	return &ControlPlaneApplyConfiguration{}
}

// WithConditions adds the given value to the Conditions field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Conditions field.
func (b *ControlPlaneApplyConfiguration) WithConditions(values ...*v1.ConditionApplyConfiguration) *ControlPlaneApplyConfiguration {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithConditions")
		}
		b.Conditions = append(b.Conditions, *values[i])
	}
	return b
}

// WithResource sets the Resource field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Resource field is set to the value of the last call.
func (b *ControlPlaneApplyConfiguration) WithResource(value *ResourceRefApplyConfiguration) *ControlPlaneApplyConfiguration {
	b.Resource = value
	return b
}

// WithPoolResource sets the PoolResource field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the PoolResource field is set to the value of the last call.
func (b *ControlPlaneApplyConfiguration) WithPoolResource(value *PoolResourceRefApplyConfiguration) *ControlPlaneApplyConfiguration {
	b.PoolResource = value
	return b
}

// WithInformers adds the given value to the Informers field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Informers field.
func (b *ControlPlaneApplyConfiguration) WithInformers(values ...*ControlPlaneInformerApplyConfiguration) *ControlPlaneApplyConfiguration {
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithInformers")
		}
		b.Informers = append(b.Informers, *values[i])
	}
	return b
}
