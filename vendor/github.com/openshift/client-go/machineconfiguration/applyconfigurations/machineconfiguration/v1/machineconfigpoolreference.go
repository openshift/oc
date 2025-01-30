// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

// MachineConfigPoolReferenceApplyConfiguration represents a declarative configuration of the MachineConfigPoolReference type for use
// with apply.
type MachineConfigPoolReferenceApplyConfiguration struct {
	Name *string `json:"name,omitempty"`
}

// MachineConfigPoolReferenceApplyConfiguration constructs a declarative configuration of the MachineConfigPoolReference type for use with
// apply.
func MachineConfigPoolReference() *MachineConfigPoolReferenceApplyConfiguration {
	return &MachineConfigPoolReferenceApplyConfiguration{}
}

// WithName sets the Name field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Name field is set to the value of the last call.
func (b *MachineConfigPoolReferenceApplyConfiguration) WithName(value string) *MachineConfigPoolReferenceApplyConfiguration {
	b.Name = &value
	return b
}
