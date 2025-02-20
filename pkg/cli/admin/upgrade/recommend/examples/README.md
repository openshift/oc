# Examples for `oc adm upgrade recommend`

Each example consists of inputs and outputs, matched by a common substring:

* `TESTCASE-cv.yaml` (input): ClusterVersion object (created by `oc get clusterversion version -o yaml`).  Lists are also supported.
* `TESTCASE.output` (output): expected output of `oc adm upgrade recommend`.
* `TESTCASE.show-outdated-releases-output` (output): expected output of `oc adm upgrade recommend --show-outdated-releases`.
* `TESTCASE.version-<VERSION>-output` (output): expected output of `oc adm upgrade recommend --to <VERSION>`.

The `TestExamples` test in [`examples_test.go`](../examples_test.go) file above validates all examples.
When the testcase is executed with a non-empty `UPDATE` environmental variable, it will update the `TESTCASE.out` fixture:

```console
$ UPDATE=yes go test -v ./pkg/cli/admin/upgrade/recommend/...
```

You can also pass the inputs to the `oc adm upgrade recommend` directly:

```console
$ oc adm upgrade recommend --mock-clusterversion=4.14.1-all-recommended-cv.yaml
```
