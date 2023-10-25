package oidc

import "strings"

// ExternalOIDCSetOrOverrideArgs sets or overrides the arguments defined in credentials plugin with
// the values passed in extraArgs iff these values are not in disallowed list.
func ExternalOIDCSetOrOverrideArgs(args []string, extraArgs []string, disallowed map[string]struct{}) []string {
	for _, arg := range extraArgs {
		pairs := strings.SplitN(arg, "=", 2)
		if _, ok := disallowed[pairs[0]]; ok {
			continue
		}

		found := false
		for i, val := range args {
			v := strings.SplitN(val, "=", 2)
			if v[0] == pairs[0] {
				args[i] = arg
				found = true
				break
			}
		}

		if !found {
			args = append(args, arg)
		}
	}

	return args
}

// ExternalOIDCExtraArgIsSet overrides the default plugin parameter or sets new one
// if it is not defined as default.
func ExternalOIDCExtraArgIsSet(extraArgs []string, param string) (bool, string) {
	if len(extraArgs) == 0 {
		return false, ""
	}

	for _, val := range extraArgs {
		values := strings.SplitN(val, "=", 2)
		if values[0] == param {
			if len(values) > 1 {
				return true, values[1]
			}
			return true, ""
		}
	}

	return false, ""
}
