package e2e

import (
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
)

var _ = g.Describe("[sig-cli][Jira:oc] oc sanity test", func() {
	g.It("should always pass", func() {
		o.Expect(true).To(o.BeTrue())
	})
	g.It("should always pass [Serial]", func() {
		o.Expect(true).To(o.BeTrue())
	})
})
