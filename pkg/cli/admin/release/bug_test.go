package release

import (
	"bytes"
	"reflect"
	"testing"
)

func TestExtractBugs(t *testing.T) {
	tests := []struct {
		input string
		bugs  BugList
		msg   string
	}{
		{
			input: " [release-4.1] Bugs , 17 43,564,: test",
			bugs: BugList{
				Bugs: []Bug{{17, Bugzilla}, {43, Bugzilla}, {564, Bugzilla}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] Bugs , 564,17 43,: test",
			bugs: BugList{
				Bugs: []Bug{{17, Bugzilla}, {43, Bugzilla}, {564, Bugzilla}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] Bug 1743564,: test",
			bugs: BugList{
				Bugs: []Bug{{1743564, Bugzilla}},
			},
			msg: "test",
		},
		{
			input: "[release-4.1] OCPBUGS-1743564: test",
			bugs: BugList{
				Bugs: []Bug{{1743564, Jira}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] OCPBUGS-17,43,564: test",
			bugs: BugList{
				Bugs: []Bug{{17, Jira}, {43, Jira}, {564, Jira}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] OCPBUGS-17,43,564,: test",
			bugs: BugList{
				Bugs: []Bug{{17, Jira}, {43, Jira}, {564, Jira}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] OCPBUGS-564,43,17,: test",
			bugs: BugList{
				Bugs: []Bug{{17, Jira}, {43, Jira}, {564, Jira}},
			},
			msg: "test",
		},
		{
			input: "OCPBUGS-17,43,564 : test",
			bugs: BugList{
				Bugs: []Bug{{17, Jira}, {43, Jira}, {564, Jira}},
			},
			msg: "test",
		},
		{
			input: "test",
			bugs:  BugList{},
			msg:   "test",
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			actualBugs, actualMsg := extractBugs(tt.input)
			if tt.msg != actualMsg {
				t.Errorf("extractBugs() actual message = %s, wanted message %s", actualMsg, tt.msg)
			}
			if !reflect.DeepEqual(actualBugs, tt.bugs) {
				t.Errorf("extractBugs() actual bugs = %v, wanted bugs %v", actualBugs, tt.bugs)
			}
		})
	}
}

func TestGetBugList(t *testing.T) {
	tests := []struct {
		input  map[string]Bug
		wanted []Bug
	}{
		{
			input: map[string]Bug{
				"jira-511": {
					ID:     511,
					Source: Jira,
				},
				"bugzilla-12": {
					ID:     12,
					Source: Bugzilla,
				},
				"jira-1": {
					ID:     1,
					Source: Jira,
				},
			},
			wanted: []Bug{
				{
					ID:     1,
					Source: Jira,
				},
				{
					ID:     12,
					Source: Bugzilla,
				},
				{
					ID:     511,
					Source: Jira,
				},
			},
		},
	}

	for _, tt := range tests {
		actual := GetBugList(tt.input)
		if !reflect.DeepEqual(actual, tt.wanted) {
			t.Errorf("getbuglist actual %v wanted %v", actual, tt.wanted)
		}
	}
}

func TestConvertJiraToBugRemoteInfo(t *testing.T) {
	tests := []struct {
		input  JiraRemoteBug
		wanted BugRemoteInfo
	}{
		{
			input: JiraRemoteBug{
				Key: "OCPBUGS-11",
				Fields: JiraRemoteFields{
					Summary: "test bug",
					Status: JiraRemoteStatus{
						Name: "Closed",
					},
					Priority: JiraRemotePriority{
						Name: "Blocker",
					},
				},
			},
			wanted: BugRemoteInfo{
				ID:       11,
				Status:   "Closed",
				Priority: "Blocker",
				Summary:  "test bug",
				Source:   Jira,
			},
		},
	}

	for _, tt := range tests {
		actual := convertJiraToBugRemoteInfo(tt.input)
		if !reflect.DeepEqual(actual, tt.wanted) {
			t.Errorf("convertjiratobugremoteinfo actual %v wanted %v", actual, tt.wanted)
		}
	}
}

func TestPrintBugs(t *testing.T) {
	tests := []struct {
		input  BugList
		wanted string
	}{
		{
			input: BugList{
				Bugs: []Bug{{1212, Bugzilla}}},
			wanted: " [Bug 1212](https://bugzilla.redhat.com/show_bug.cgi?id=1212):",
		},
		{
			input: BugList{
				Bugs: []Bug{{1212, Jira}}},
			wanted: " [OCPBUGS-1212](https://issues.redhat.com/browse/OCPBUGS-1212):",
		},
		{
			input:  BugList{[]Bug{{1212, Bugzilla}, {22, Bugzilla}}},
			wanted: " [Bug 1212](https://bugzilla.redhat.com/show_bug.cgi?id=1212), [22](https://bugzilla.redhat.com/show_bug.cgi?id=22):",
		},
		{
			input:  BugList{[]Bug{{1212, Jira}, {22, Jira}}},
			wanted: " [OCPBUGS-1212](https://issues.redhat.com/browse/OCPBUGS-1212), [22](https://issues.redhat.com/browse/OCPBUGS-22):",
		},
		{
			input:  BugList{[]Bug{{1212, Jira}, {22, Bugzilla}}},
			wanted: " [OCPBUGS-1212](https://issues.redhat.com/browse/OCPBUGS-1212), [22](https://bugzilla.redhat.com/show_bug.cgi?id=22):",
		},
		{
			input:  BugList{},
			wanted: "",
		},
	}

	for _, tt := range tests {
		var b bytes.Buffer
		t.Run("", func(t *testing.T) {
			tt.input.PrintBugs(&b)
			actual := b.String()
			if actual != tt.wanted {
				t.Errorf("printBugs() actual print %s wanted %s", actual, tt.wanted)
			}
		})
	}
}
