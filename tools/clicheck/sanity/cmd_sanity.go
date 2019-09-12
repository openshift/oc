package sanity

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

type CmdCheck func(cmd *cobra.Command) []error

var (
	AllCmdChecks = []CmdCheck{
		CheckLongDesc,
		CheckExamples,
	}
)

func CheckCmdTree(cmd *cobra.Command, checks []CmdCheck, skip []string) []error {
	cmdPath := cmd.CommandPath()

	for _, skipCmdPath := range skip {
		if cmdPath == skipCmdPath {
			fmt.Fprintf(os.Stdout, "-----+ skipping command %s\n", cmdPath)
			return []error{}
		}
	}

	errors := []error{}

	if cmd.HasSubCommands() {
		for _, subCmd := range cmd.Commands() {
			errors = append(errors, CheckCmdTree(subCmd, checks, skip)...)
		}
	}

	fmt.Fprintf(os.Stdout, "-----+ checking command %s\n", cmdPath)

	for _, check := range checks {
		if err := check(cmd); err != nil && len(err) > 0 {
			errors = append(errors, err...)
		}
	}

	return errors
}

func CheckLongDesc(cmd *cobra.Command) []error {
	cmdPath := cmd.CommandPath()
	long := cmd.Long
	if len(long) > 0 {
		if strings.Trim(long, " \t\n") != long {
			return []error{fmt.Errorf(`command %q: long description is not normalized
 ↳ make sure you are calling templates.LongDesc (from pkg/cmd/templates) before assigning cmd.Long`, cmdPath)}
		}
	}
	return nil
}

func CheckExamples(cmd *cobra.Command) []error {
	cmdPath := cmd.CommandPath()
	examples := cmd.Example
	errors := []error{}
	if len(examples) > 0 {
		for _, line := range strings.Split(examples, "\n") {
			if !strings.HasPrefix(line, templates.Indentation) {
				errors = append(errors, fmt.Errorf(`command %q: examples are not normalized
 ↳ make sure you are calling templates.Examples (from pkg/cmd/templates) before assigning cmd.Example`, cmdPath))
			}
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
				errors = append(errors, fmt.Errorf(`command %q: we use # to start comments in examples instead of //`, cmdPath))
			}
		}
	}
	return errors
}
