package regeneratetoplevel

import (
	"testing"
)

func TestNonDryRun(t *testing.T) {
	tests, err := AllTestsInDir("testdata")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range tests {
		currTest := simpleRegenerateTest{
			regenerateTest: test,
		}
		t.Run(test.Name, currTest.Test)
	}
}
