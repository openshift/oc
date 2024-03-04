# Examples for `oc adm upgrade status`

Each example consists of five inputs and one output, matched by a common substring:
1. `TESTCASE-cv.yaml`(input): ClusterVersion object (created by `oc get clusterversion version -o yaml`)
2. `TESTCASE-co.yaml`(input): list of ClusterOperators (created by `oc get clusteroperators -o yaml`)
3.  `TESTCASE-mc.yaml`(input): list of MachineConfigs (created by `oc get oc get machineconfigs -o yaml`)
4.  `TESTCASE-mcp.yaml`(input): list of MachineConfigPools (created by `oc get machineconfigpools -o yaml`)
5.  `TESTCASE-node.yaml`(input): list of Nodes (created by `oc get nodes -o yaml`)
6. `TESTCASE.out`(output): expected output of `oc adm upgrade status` for the two outputs

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
