// Code generated by applyconfiguration-gen. DO NOT EDIT.

package internal

import (
	fmt "fmt"
	sync "sync"

	typed "sigs.k8s.io/structured-merge-diff/v4/typed"
)

func Parser() *typed.Parser {
	parserOnce.Do(func() {
		var err error
		parser, err = typed.NewParser(schemaYAML)
		if err != nil {
			panic(fmt.Sprintf("Failed to parse schema: %v", err))
		}
	})
	return parser
}

var parserOnce sync.Once
var parser *typed.Parser
var schemaYAML = typed.YAMLObject(`types:
- name: com.github.openshift.api.security.v1.AllowedFlexVolume
  map:
    fields:
    - name: driver
      type:
        scalar: string
      default: ""
- name: com.github.openshift.api.security.v1.FSGroupStrategyOptions
  map:
    fields:
    - name: ranges
      type:
        list:
          elementType:
            namedType: com.github.openshift.api.security.v1.IDRange
          elementRelationship: atomic
    - name: type
      type:
        scalar: string
- name: com.github.openshift.api.security.v1.IDRange
  map:
    fields:
    - name: max
      type:
        scalar: numeric
    - name: min
      type:
        scalar: numeric
- name: com.github.openshift.api.security.v1.RangeAllocation
  map:
    fields:
    - name: apiVersion
      type:
        scalar: string
    - name: data
      type:
        scalar: string
    - name: kind
      type:
        scalar: string
    - name: metadata
      type:
        namedType: io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta
      default: {}
    - name: range
      type:
        scalar: string
      default: ""
- name: com.github.openshift.api.security.v1.RunAsUserStrategyOptions
  map:
    fields:
    - name: type
      type:
        scalar: string
    - name: uid
      type:
        scalar: numeric
    - name: uidRangeMax
      type:
        scalar: numeric
    - name: uidRangeMin
      type:
        scalar: numeric
- name: com.github.openshift.api.security.v1.SELinuxContextStrategyOptions
  map:
    fields:
    - name: seLinuxOptions
      type:
        namedType: io.k8s.api.core.v1.SELinuxOptions
    - name: type
      type:
        scalar: string
- name: com.github.openshift.api.security.v1.SecurityContextConstraints
  map:
    fields:
    - name: allowHostDirVolumePlugin
      type:
        scalar: boolean
      default: false
    - name: allowHostIPC
      type:
        scalar: boolean
      default: false
    - name: allowHostNetwork
      type:
        scalar: boolean
      default: false
    - name: allowHostPID
      type:
        scalar: boolean
      default: false
    - name: allowHostPorts
      type:
        scalar: boolean
      default: false
    - name: allowPrivilegeEscalation
      type:
        scalar: boolean
    - name: allowPrivilegedContainer
      type:
        scalar: boolean
      default: false
    - name: allowedCapabilities
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
    - name: allowedFlexVolumes
      type:
        list:
          elementType:
            namedType: com.github.openshift.api.security.v1.AllowedFlexVolume
          elementRelationship: atomic
    - name: allowedUnsafeSysctls
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
    - name: apiVersion
      type:
        scalar: string
    - name: defaultAddCapabilities
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
    - name: defaultAllowPrivilegeEscalation
      type:
        scalar: boolean
    - name: forbiddenSysctls
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
    - name: fsGroup
      type:
        namedType: com.github.openshift.api.security.v1.FSGroupStrategyOptions
      default: {}
    - name: groups
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
    - name: kind
      type:
        scalar: string
    - name: metadata
      type:
        namedType: io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta
      default: {}
    - name: priority
      type:
        scalar: numeric
    - name: readOnlyRootFilesystem
      type:
        scalar: boolean
      default: false
    - name: requiredDropCapabilities
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
    - name: runAsUser
      type:
        namedType: com.github.openshift.api.security.v1.RunAsUserStrategyOptions
      default: {}
    - name: seLinuxContext
      type:
        namedType: com.github.openshift.api.security.v1.SELinuxContextStrategyOptions
      default: {}
    - name: seccompProfiles
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
    - name: supplementalGroups
      type:
        namedType: com.github.openshift.api.security.v1.SupplementalGroupsStrategyOptions
      default: {}
    - name: userNamespaceLevel
      type:
        scalar: string
      default: AllowHostLevel
    - name: users
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
    - name: volumes
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: atomic
- name: com.github.openshift.api.security.v1.SupplementalGroupsStrategyOptions
  map:
    fields:
    - name: ranges
      type:
        list:
          elementType:
            namedType: com.github.openshift.api.security.v1.IDRange
          elementRelationship: atomic
    - name: type
      type:
        scalar: string
- name: io.k8s.api.core.v1.SELinuxOptions
  map:
    fields:
    - name: level
      type:
        scalar: string
    - name: role
      type:
        scalar: string
    - name: type
      type:
        scalar: string
    - name: user
      type:
        scalar: string
- name: io.k8s.apimachinery.pkg.apis.meta.v1.FieldsV1
  map:
    elementType:
      scalar: untyped
      list:
        elementType:
          namedType: __untyped_atomic_
        elementRelationship: atomic
      map:
        elementType:
          namedType: __untyped_deduced_
        elementRelationship: separable
- name: io.k8s.apimachinery.pkg.apis.meta.v1.ManagedFieldsEntry
  map:
    fields:
    - name: apiVersion
      type:
        scalar: string
    - name: fieldsType
      type:
        scalar: string
    - name: fieldsV1
      type:
        namedType: io.k8s.apimachinery.pkg.apis.meta.v1.FieldsV1
    - name: manager
      type:
        scalar: string
    - name: operation
      type:
        scalar: string
    - name: subresource
      type:
        scalar: string
    - name: time
      type:
        namedType: io.k8s.apimachinery.pkg.apis.meta.v1.Time
- name: io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta
  map:
    fields:
    - name: annotations
      type:
        map:
          elementType:
            scalar: string
    - name: creationTimestamp
      type:
        namedType: io.k8s.apimachinery.pkg.apis.meta.v1.Time
    - name: deletionGracePeriodSeconds
      type:
        scalar: numeric
    - name: deletionTimestamp
      type:
        namedType: io.k8s.apimachinery.pkg.apis.meta.v1.Time
    - name: finalizers
      type:
        list:
          elementType:
            scalar: string
          elementRelationship: associative
    - name: generateName
      type:
        scalar: string
    - name: generation
      type:
        scalar: numeric
    - name: labels
      type:
        map:
          elementType:
            scalar: string
    - name: managedFields
      type:
        list:
          elementType:
            namedType: io.k8s.apimachinery.pkg.apis.meta.v1.ManagedFieldsEntry
          elementRelationship: atomic
    - name: name
      type:
        scalar: string
    - name: namespace
      type:
        scalar: string
    - name: ownerReferences
      type:
        list:
          elementType:
            namedType: io.k8s.apimachinery.pkg.apis.meta.v1.OwnerReference
          elementRelationship: associative
          keys:
          - uid
    - name: resourceVersion
      type:
        scalar: string
    - name: selfLink
      type:
        scalar: string
    - name: uid
      type:
        scalar: string
- name: io.k8s.apimachinery.pkg.apis.meta.v1.OwnerReference
  map:
    fields:
    - name: apiVersion
      type:
        scalar: string
      default: ""
    - name: blockOwnerDeletion
      type:
        scalar: boolean
    - name: controller
      type:
        scalar: boolean
    - name: kind
      type:
        scalar: string
      default: ""
    - name: name
      type:
        scalar: string
      default: ""
    - name: uid
      type:
        scalar: string
      default: ""
    elementRelationship: atomic
- name: io.k8s.apimachinery.pkg.apis.meta.v1.Time
  scalar: untyped
- name: __untyped_atomic_
  scalar: untyped
  list:
    elementType:
      namedType: __untyped_atomic_
    elementRelationship: atomic
  map:
    elementType:
      namedType: __untyped_atomic_
    elementRelationship: atomic
- name: __untyped_deduced_
  scalar: untyped
  list:
    elementType:
      namedType: __untyped_atomic_
    elementRelationship: atomic
  map:
    elementType:
      namedType: __untyped_deduced_
    elementRelationship: separable
`)
