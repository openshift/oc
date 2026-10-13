package release

import (
	"bytes"
	"reflect"
	"testing"
)

func TestExtractBugs(t *testing.T) {
	tests := []struct {
		input string
		bugs  RefList
		msg   string
	}{
		{
			input: " [release-4.1] Bugs , 17 43,564,: test",
			bugs: RefList{
				Refs: []Ref{{"17", Bugzilla}, {"43", Bugzilla}, {"564", Bugzilla}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] Bugs , 564,17 43,: test",
			bugs: RefList{
				Refs: []Ref{{"17", Bugzilla}, {"43", Bugzilla}, {"564", Bugzilla}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] Bug 1743564,: test",
			bugs: RefList{
				Refs: []Ref{{"1743564", Bugzilla}},
			},
			msg: "test",
		},
		{
			input: "[release-4.1] OCPBUGS-1743564: test",
			bugs: RefList{
				Refs: []Ref{{"OCPBUGS-1743564", Jira}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] OCPBUGS-17,OCPBUGS-43,OCPBUGS-564: test",
			bugs: RefList{
				Refs: []Ref{{"OCPBUGS-17", Jira}, {"OCPBUGS-43", Jira}, {"OCPBUGS-564", Jira}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] OCPBUGS-17,OCPBUGS-43,OCPBUGS-564,: test",
			bugs: RefList{
				Refs: []Ref{{"OCPBUGS-17", Jira}, {"OCPBUGS-43", Jira}, {"OCPBUGS-564", Jira}},
			},
			msg: "test",
		},
		{
			input: " [release-4.1] OCPBUGS-564,OCPBUGS-43,OCPBUGS-17,: test",
			bugs: RefList{
				Refs: []Ref{{"OCPBUGS-17", Jira}, {"OCPBUGS-43", Jira}, {"OCPBUGS-564", Jira}},
			},
			msg: "test",
		},
		{
			input: "OCPBUGS-17,OCPBUGS-43,OCPBUGS-564 : test",
			bugs: RefList{
				Refs: []Ref{{"OCPBUGS-17", Jira}, {"OCPBUGS-43", Jira}, {"OCPBUGS-564", Jira}},
			},
			msg: "test",
		},
		{
			input: "test",
			bugs:  RefList{},
			msg:   "test",
		},
		{
			input: "OCPBUGS-17 - test",
			bugs: RefList{
				Refs: []Ref{{"OCPBUGS-17", Jira}},
			},
			msg: "test",
		},
		{
			input: "OCPBUGS-17,OCPBUGS-43,OCPBUGS-564 - test",
			bugs: RefList{
				Refs: []Ref{{"OCPBUGS-17", Jira}, {"OCPBUGS-43", Jira}, {"OCPBUGS-564", Jira}},
			},
			msg: "test",
		},
		{
			input: "ocpbugs-17: test",
			bugs:  RefList{},
			msg:   "ocpbugs-17: test",
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			actualBugs, actualMsg := extractRefs(tt.input)
			if tt.msg != actualMsg {
				t.Errorf("extractBugs() actual message = %s, wanted message %s", actualMsg, tt.msg)
			}
			if !reflect.DeepEqual(actualBugs, tt.bugs) {
				t.Errorf("extractBugs() actual bugs = %v, wanted bugs %v", actualBugs, tt.bugs)
			}
		})
	}
}

func TestGetRefList(t *testing.T) {
	tests := []struct {
		input  map[string]Ref
		wanted []Ref
	}{
		{
			input: map[string]Ref{
				"jira-511": {
					ID:     "511",
					Source: Jira,
				},
				"bugzilla-12": {
					ID:     "12",
					Source: Bugzilla,
				},
				"jira-1": {
					ID:     "1",
					Source: Jira,
				},
			},
			wanted: []Ref{
				{
					ID:     "1",
					Source: Jira,
				},
				{
					ID:     "12",
					Source: Bugzilla,
				},
				{
					ID:     "511",
					Source: Jira,
				},
			},
		},
	}

	for _, tt := range tests {
		actual := GetRefList(tt.input)
		if !reflect.DeepEqual(actual, tt.wanted) {
			t.Errorf("getbuglist actual %v wanted %v", actual, tt.wanted)
		}
	}
}

func TestConvertJiraToRefRemoteInfo(t *testing.T) {
	tests := []struct {
		input  JiraRemoteRef
		wanted RefRemoteInfo
	}{
		{
			input: JiraRemoteRef{
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
			wanted: RefRemoteInfo{
				ID:       "OCPBUGS-11",
				Status:   "Closed",
				Priority: "Blocker",
				Summary:  "test bug",
				Source:   Jira,
			},
		},
	}

	for _, tt := range tests {
		actual := convertJiraToRefRemoteInfo(tt.input)
		if !reflect.DeepEqual(actual, tt.wanted) {
			t.Errorf("convertjiratobugremoteinfo actual %v wanted %v", actual, tt.wanted)
		}
	}
}

func TestPrintRefs(t *testing.T) {
	tests := []struct {
		input  RefList
		wanted string
	}{
		{
			input: RefList{
				Refs: []Ref{{"1212", Bugzilla}}},
			wanted: " [Bug 1212](https://bugzilla.redhat.com/show_bug.cgi?id=1212):",
		},
		{
			input: RefList{
				Refs: []Ref{{"OCPBUGS-1212", Jira}}},
			wanted: " [OCPBUGS-1212](https://issues.redhat.com/browse/OCPBUGS-1212):",
		},
		{
			input:  RefList{[]Ref{{"1212", Bugzilla}, {"22", Bugzilla}}},
			wanted: " [Bug 1212](https://bugzilla.redhat.com/show_bug.cgi?id=1212), [22](https://bugzilla.redhat.com/show_bug.cgi?id=22):",
		},
		{
			input:  RefList{[]Ref{{"OCPBUGS-1212", Jira}, {"OCPBUGS-22", Jira}}},
			wanted: " [OCPBUGS-1212](https://issues.redhat.com/browse/OCPBUGS-1212), [OCPBUGS-22](https://issues.redhat.com/browse/OCPBUGS-22):",
		},
		{
			input:  RefList{[]Ref{{"ABC-1212", Jira}, {"22", Bugzilla}}},
			wanted: " [ABC-1212](https://issues.redhat.com/browse/ABC-1212), [22](https://bugzilla.redhat.com/show_bug.cgi?id=22):",
		},
		{
			input:  RefList{},
			wanted: "",
		},
	}

	for _, tt := range tests {
		var b bytes.Buffer
		t.Run("", func(t *testing.T) {
			tt.input.PrintRefs(&b)
			actual := b.String()
			if actual != tt.wanted {
				t.Errorf("printBugs() actual print:\n|%s|\nwanted:\n|%s|", actual, tt.wanted)
			}
		})
	}
}
