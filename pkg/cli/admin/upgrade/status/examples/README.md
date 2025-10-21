# Examples for `oc adm upgrade status`

Each example consists of multiple inputs and outputs, matched by a common substring:
* `TESTCASE-cv.yaml`(input): ClusterVersion object (created by `oc get clusterversion version -o yaml`)
* `TESTCASE-co.yaml`(input): list of ClusterOperators (created by `oc get clusteroperators -o yaml`)
* `TESTCASE-mc.yaml`(input): list of MachineConfigs (created by `oc get machineconfigs -o yaml`)
* `TESTCASE-mcp.yaml`(input): list of MachineConfigPools (created by `oc get machineconfigpools -o yaml`)
* `TESTCASE-node.yaml`(input): list of Nodes (created by `oc get nodes -o yaml`)
* `TESTCASE-alerts.json` (optional input): current alerts (created by `OC_ENABLE_CMD_INSPECT_ALERTS=true oc adm inspect-alerts`)
* `TESTCASE.output`(output): expected output of `oc adm upgrade status`
* `TESTCASE.detailed-output`(output): expected output of `oc adm upgrade status --details=all`

The `TestExamples` test in `examples_test.go` file above validates all examples. When the testcase
is executed with a non-empty `UPDATE` environmental variable, it will update the `TESTCASE.out`
fixture:

```console
$ UPDATE=yes go test -v ./pkg/cli/admin/upgrade/status/...
```

You can also pass the inputs to the `oc adm upgrade status` directly:

```
$ oc adm upgrade status --mock-clusterversion=not-upgrading-cv.yaml
```
