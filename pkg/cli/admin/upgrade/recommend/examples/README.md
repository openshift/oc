# Examples for `oc adm upgrade recommend`

Each example consists of inputs and outputs, matched by a common substring:

* `TESTCASE-cv.yaml` (input): ClusterVersion object (created by `oc get clusterversion version -o yaml`).  Lists are also supported.
* `TESTCASE.output` (output): expected output of `oc adm upgrade recommend`.
* `TESTCASE.include-not-recommended-output` (output): expected output of `oc adm upgrade recommend --include-not-recommended`.

The `TestExamples` test in [`examples_test.go`](../examples_test.go) file above validates all examples.
When the testcase is executed with a non-empty `UPDATE` environmental variable, it will update the `TESTCASE.out` fixture:

```console
$ UPDATE=yes go test -v ./pkg/cli/admin/upgrade/recommend/...
```

You can also pass the inputs to the `oc adm upgrade recommend` directly:

```console
$ oc adm upgrade recommend --mock-clusterversion=4.14.1-both-available-and-conditional-cv.yaml
```
