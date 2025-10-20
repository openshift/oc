package release

import (
	"reflect"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/diff"
)

type fakeGit struct {
	input  string
	squash bool
}

func (g fakeGit) exec(commands ...string) (string, error) {
	if commands[1] == "--merges" && g.squash {
		return "", nil
	}
	return g.input, nil
}

func Test_mergeLogForRepo(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		repo              string
		from              string
		to                string
		squash            bool
		want              []MergeCommit
		wantElidedCommits int
		wantErr           bool
	}{
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1eBug 1743564: test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "1743564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1eBug 1743564: test [trailing]",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "1743564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test [trailing]",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1e[release-4.1] Bug 1743564: test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "1743564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1e [release-4.1] Bug 1743564: test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "1743564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1e [release-4.1] Bug 1743564 : test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "1743564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1e [release-4.1] Bugs 1743564 : test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "1743564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1e [release-4.1] Bugs 1743564,: test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "1743564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1e [release-4.1] Bugs , 17 43,564,: test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "17",
								Source: Bugzilla,
							},
							{
								ID:     "43",
								Source: Bugzilla,
							},
							{
								ID:     "564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1e [release-4.1] bugs , 17 43,564,: test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "17",
								Source: Bugzilla,
							},
							{
								ID:     "43",
								Source: Bugzilla,
							},
							{
								ID:     "564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "test",
				},
			},
		},
		{
			input: "abc\x1e1\x1eMerge pull request #145 from\x1e [release 4.1] bugs , 17 43,564,: test",
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs:    RefList{},
					Subject: "[release 4.1] bugs , 17 43,564,: test",
				},
			},
		},
		{
			input:  "abc\x1e1\x1eBugs 1743564: fix typo (#145)\x1e * fix typo",
			squash: true,
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "1743564",
								Source: Bugzilla,
							},
						},
					},
					Subject: "fix typo (#145)",
				},
			},
		},
		{
			input:  "abc\x1e1\x1eOCPBUGS-1743564: fix typo (#145)\x1e * fix typo",
			squash: true,
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "OCPBUGS-1743564",
								Source: Jira,
							},
						},
					},
					Subject: "fix typo (#145)",
				},
			},
		},
		{
			input:  "abc\x1e1\x1efix vendoring from #123 (#145)\x1e * fix vendoring from #123",
			squash: true,
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs:    RefList{},
					Subject: "fix vendoring from #123 (#145)",
				},
			},
		},
		{
			input:  "abc\x1e1\x1eOCPBUGS-1743564, NE-123 ABC-789,,, XYZ-123: fix typo (#145)\x1e * fix typo",
			squash: true,
			want: []MergeCommit{
				{
					ParentCommits: []string{}, Commit: "abc", PullRequest: 145, CommitDate: time.Unix(1, 0).UTC(),
					Refs: RefList{
						Refs: []Ref{
							{
								ID:     "ABC-789",
								Source: Jira,
							},
							{
								ID:     "NE-123",
								Source: Jira,
							},
							{
								ID:     "OCPBUGS-1743564",
								Source: Jira,
							},
							{
								ID:     "XYZ-123",
								Source: Jira,
							},
						},
					},
					Subject: "fix typo (#145)",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := fakeGit{input: tt.input, squash: tt.squash}
			got, elidedCommits, err := mergeLogForRepo(g, tt.repo, "a", "b")
			if (err != nil) != tt.wantErr {
				t.Errorf("mergeLogForRepo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeLogForRepo(): %s", diff.ObjectGoPrintSideBySide(tt.want, got))
			}
			if elidedCommits != tt.wantElidedCommits {
				t.Errorf("mergeLogForRepo(): %d elided commits report differs from expected %d", elidedCommits, tt.wantElidedCommits)
			}
		})
	}
}
