# Examples for `oc adm upgrade recommend`

Each testcase is anchored by an `xxx-cv.yaml`, where `xxx` is a common substring that identifies the testcase. The `-cv.yaml` file describes the ClusterVersion object essential to the functionality of `oc adm upgrade recommend`

**INPUTS**
---
* `TESTCASE-cv.yaml`: ClusterVersion object (required, created by `oc get clusterversion version -o yaml`).  Lists are also supported.
* `TESTCASE-featuregate.yaml`: FeatureGate object (optional, created by `oc get featuregate cluster -o yaml`). Lists are NOT supported.
* `TESTCASE-infrastructure.yaml`: Infrastructure object (optional, created by `oc get infrastructure cluster -o yaml`). Lists are NOT supported
* `TESTCASE-alerts.json`: Alerts currently present in the cluster (optional, created by `OC_ENABLE_CMD_INSPECT_ALERTS=true oc adm inspect-alerts`)

**OUTPUTS**
---
* `TESTCASE.output`: expected output of `oc adm upgrade recommend`.
* `TESTCASE.show-outdated-releases-output`: expected output of `oc adm upgrade recommend --show-outdated-releases`.
* `TESTCASE.version-<VERSION>-output`: expected output of `oc adm upgrade recommend --to <VERSION>`.

The `TestExamples` test in [`examples_test.go`](../examples_test.go) file above validates all examples.

**TESTING**
---
* When the testcase is executed with a non-empty `UPDATE` environmental variable, it will update the `TESTCASE.out` fixture:
```console
$ UPDATE=yes go test -v ./pkg/cli/admin/upgrade/recommend/...
```

* You can also pass the inputs to the `oc adm upgrade recommend` directly:

```console
$ oc adm upgrade recommend --mock-clusterversion=4.14.1-all-recommended-cv.yaml
```
