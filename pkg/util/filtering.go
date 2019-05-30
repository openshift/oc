package util

import (
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
)

func AcceptString(allowedValues sets.String, currValue string) bool {
	// check for an anti-match
	if allowedValues.Has("-" + currValue) {
		return false
	}
	for _, allowedValue := range allowedValues.UnsortedList() {
		if !strings.HasSuffix(allowedValue, "*") || !strings.HasPrefix(allowedValue, "-") {
			continue
		}
		if strings.HasPrefix("-"+currValue, allowedValue[:len(allowedValue)-1]) {
			return false
		}
	}

	// if all values are negation, assume * by default
	allValuesNegative := true
	for _, allowedValue := range allowedValues.UnsortedList() {
		if !strings.HasPrefix(allowedValue, "-") {
			allValuesNegative = false
			break
		}
	}
	if allValuesNegative {
		return true
	}

	if allowedValues.Has(currValue) {
		return true
	}
	for _, allowedValue := range allowedValues.UnsortedList() {
		if !strings.HasSuffix(allowedValue, "*") || strings.HasPrefix(allowedValue, "-") {
			continue
		}
		if strings.HasPrefix(currValue, allowedValue[:len(allowedValue)-1]) {
			return true
		}
	}

	return false
}
