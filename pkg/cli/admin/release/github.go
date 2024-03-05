package release

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openshift/oc/pkg/version"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

type github struct {
	url   string
	token string
	http  *http.Client
}

func NewGitHubClient(url, tokenPath string) (*github, error) {
	httpClient := &http.Client{
		Timeout: 5 * time.Minute,
	}
	var token []byte
	if tokenPath != "" {
		var err error
		token, err = os.ReadFile(tokenPath)
		if err != nil {
			return nil, fmt.Errorf("Failed to read github token: %w", err)
		}
	}
	return &github{
		url:   url,
		token: string(bytes.TrimSpace(token)),
		http:  httpClient,
	}, nil
}

func GitHubCommitToMergeCommitSingle(rCommit RepositoryCommit, squash bool) (*MergeCommit, error) {
	// check if merge commit is squash is false
	if !squash && len(rCommit.Parents) == 1 {
		return nil, nil
	}
	mCommit := &MergeCommit{
		CommitDate: rCommit.Commit.Committer.Date,
		Commit:     rCommit.SHA,
	}
	// github only sends the parents in the full RepositoryCommit
	for _, parent := range rCommit.Parents {
		mCommit.ParentCommits = append(mCommit.ParentCommits, parent.SHA)
	}

	fullMsg := rCommit.Commit.Message
	var mergeMsg, msg string
	if !squash {
		splitMessage := strings.Split(fullMsg, "\n\n")
		mergeMsg = splitMessage[0]
		msg = strings.TrimPrefix(fullMsg, mergeMsg+"\n\n")
	} else {
		mergeMsg = fullMsg
		msg = fullMsg
	}
	mCommit.Refs, msg = extractRefs(msg)
	msg = strings.TrimSpace(msg)
	msg = strings.SplitN(msg, "\n", 2)[0]

	var m []string
	if !squash {
		m = rePR.FindStringSubmatch(mergeMsg)
	} else {
		m = squashRePR.FindStringSubmatch(mergeMsg)
	}

	if m == nil || len(m) < 2 {
		klog.V(2).Infof("Omitted commit %s which has no pull-request", mCommit.Commit)
		return nil, nil
	}
	var err error
	mCommit.PullRequest, err = strconv.Atoi(m[1])
	if err != nil {
		return nil, fmt.Errorf("could not extract PR number from %q: %v", mergeMsg, err)
	}
	if len(msg) == 0 {
		msg = "Merge"
	}

	mCommit.Subject = msg
	return mCommit, nil
}

func GitHubCommitsToMergeCommits(gCommits []RepositoryCommit, squash bool) ([]MergeCommit, error) {
	mCommits := []MergeCommit{}
	for _, fullCommit := range gCommits {
		mCommit, err := GitHubCommitToMergeCommitSingle(fullCommit, squash)
		if err != nil {
			return nil, err
		}
		if mCommit != nil {
			mCommits = append(mCommits, *mCommit)
		}
	}
	if len(mCommits) == 0 {
		if squash {
			// this shouldn't be possible
			return nil, fmt.Errorf("No commits found")
		}
		return GitHubCommitsToMergeCommits(gCommits, true)
	}

	// to emulate the "first parent" flag of github, we must walk through the commits
	if !squash {
		finalCommits := []MergeCommit{}
		mCommitMap := make(map[string]MergeCommit)
		for _, commit := range mCommits {
			mCommitMap[commit.Commit] = commit
		}
		// reverse gCommits so we can follow the first parent tree
		for i, j := 0, len(gCommits)-1; i < j; i, j = i+1, j-1 {
			gCommits[i], gCommits[j] = gCommits[j], gCommits[i]
		}
		// we need to follow parent commits of merge to identify if their parents matched the previous parent
		prevParents := sets.New[string]()
		prevParents.Insert(gCommits[0].SHA)
		for _, gCommit := range gCommits {
			if prevParents.Has(gCommit.SHA) {
				prevParents.Insert(gCommit.Parents[0].SHA)
				if commit, ok := mCommitMap[gCommit.SHA]; ok {
					finalCommits = append(finalCommits, commit)
				}
			}
		}
		return finalCommits, nil
	}
	// mimic git log behavior by changing to descending timed commits
	for i, j := 0, len(mCommits)-1; i < j; i, j = i+1, j-1 {
		mCommits[i], mCommits[j] = mCommits[j], mCommits[i]
	}
	return mCommits, nil
}

func (g *github) MergeLogForRepo(repoURL, from, to string) ([]MergeCommit, error) {
	if from == to {
		return nil, nil
	}
	splitRepo := strings.Split(repoURL, "/")
	if len(splitRepo) < 3 {
		return nil, fmt.Errorf("Invalid repo URL: %s", repoURL)
	}
	org, repo := splitRepo[len(splitRepo)-2], splitRepo[len(splitRepo)-1]
	allCommits, err := g.CompareTwoCommits(org, repo, from, to)
	if err != nil {
		return nil, fmt.Errorf("Failed to compare commits for %s/%s %s...%s: %w", org, repo, from, to, err)
	}
	mergeCommits, err := GitHubCommitsToMergeCommits(allCommits.Commits, false)
	if err != nil {
		return nil, err
	}
	return mergeCommits, nil
}

func (g github) CompareTwoCommits(org, repo, from, to string) (*ComparedCommits, error) {
	commits := &ComparedCommits{}
	baseHead := fmt.Sprintf("%s...%s", from, to)
	err := g.readPaginatedResultsWithValues(
		fmt.Sprintf("/repos/%s/%s/compare/%s", org, repo, baseHead),
		url.Values{
			"per_page": []string{"100"},
		},
		"",
		org,
		func() interface{} { // newObj
			return &ComparedCommits{}
		},
		func(obj interface{}) {
			commits.Commits = append(commits.Commits, (obj.(*ComparedCommits)).Commits...)
		},
	)
	if err != nil {
		return nil, err
	}
	return commits, nil
}

const githubApiVersion = "2022-11-28"
const maxSleepTime = 2 * time.Minute

func (g *github) authHeader() string {
	if g.token == "" {
		return ""
	}
	if len(g.token) == 0 {
		return ""
	}
	return fmt.Sprintf("Bearer %s", g.token)
}

func (g *github) userAgent() string {
	clientVersion, reportVersion, _ := version.ExtractVersion()
	if reportVersion != "" {
		return "oc/" + reportVersion
	}
	return "oc/" + clientVersion.String()
}

func (g *github) doRequest(ctx context.Context, method, path, accept, org string, body interface{}) (*http.Response, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, buf)
	if err != nil {
		return nil, fmt.Errorf("failed creating new request: %w", err)
	}
	// We do not make use of the Set() method to set this header because
	// the header name `X-GitHub-Api-Version` is non-canonical in nature.
	//
	// See https://pkg.go.dev/net/http#Header.Set for more info.
	req.Header["X-GitHub-Api-Version"] = []string{githubApiVersion}
	klog.V(5).Infof("Using GitHub REST API Version: %s", githubApiVersion)
	if header := g.authHeader(); len(header) > 0 {
		req.Header.Set("Authorization", header)
	}
	if accept == "" {
		req.Header.Add("Accept", "application/vnd.github.v3+json")
	} else {
		req.Header.Add("Accept", accept)
	}
	if userAgent := g.userAgent(); userAgent != "" {
		req.Header.Add("User-Agent", userAgent)
	}
	if org != "" {
		req = req.WithContext(context.WithValue(req.Context(), "X-OC-GITHUB-ORG", org))
	}
	// Disable keep-alive so that we don't get flakes when GitHub closes the
	// connection prematurely.
	// https://go-review.googlesource.com/#/c/3210/ fixed it for GET, but not
	// for POST.
	req.Close = true

	return g.http.Do(req)
}

func (g *github) requestRetryWithContext(ctx context.Context, method, path, accept, org string, body interface{}) (*http.Response, error) {
	var resp *http.Response
	var err error
	backoff := time.Second * 2
	for retries := 0; retries < 8; retries++ {
		if retries > 0 && resp != nil {
			resp.Body.Close()
		}
		resp, err = g.doRequest(ctx, method, g.url+path, accept, org, body)
		if err == nil {
			if resp.StatusCode == 404 && retries < 2 {
				// Retry 404s a couple times. Sometimes GitHub is inconsistent in
				// the sense that they send us an event such as "PR opened" but an
				// immediate request to GET the PR returns 404. We don't want to
				// retry more than a couple times in this case, because a 404 may
				// be caused by a bad API call and we'll just burn through API
				// tokens.
				klog.V(6).Infof("Retrying 404 with backoff of %s", backoff.String())
				time.Sleep(backoff)
				backoff *= 2
			} else if resp.StatusCode == 403 {
				if resp.Header.Get("X-RateLimit-Remaining") == "0" {
					// If we are out of API tokens, sleep first. The X-RateLimit-Reset
					// header tells us the time at which we can request again.
					var t int
					if t, err = strconv.Atoi(resp.Header.Get("X-RateLimit-Reset")); err == nil {
						// Sleep an extra second plus how long GitHub wants us to
						// sleep. If it's going to take too long, then break.
						sleepTime := time.Until(time.Unix(int64(t), 0)) + time.Second
						if sleepTime < maxSleepTime {
							klog.V(5).Infof("Retrying after token budget reset with backoff %s", sleepTime.String())
							time.Sleep(sleepTime)
						} else {
							err = fmt.Errorf("sleep time for token reset exceeds max sleep time (%v > %v)", sleepTime, maxSleepTime)
							resp.Body.Close()
							break
						}
					} else {
						err = fmt.Errorf("failed to parse rate limit reset unix time %q: %w", resp.Header.Get("X-RateLimit-Reset"), err)
						resp.Body.Close()
						break
					}
				} else if rawTime := resp.Header.Get("Retry-After"); rawTime != "" && rawTime != "0" {
					// If we are getting abuse rate limited, we need to wait or
					// else we risk continuing to make the situation worse
					var t int
					if t, err = strconv.Atoi(rawTime); err == nil {
						// Sleep an extra second plus how long GitHub wants us to
						// sleep. If it's going to take too long, then break.
						sleepTime := time.Duration(t+1) * time.Second
						if sleepTime < maxSleepTime {
							klog.V(5).Infof("Retrying after abuse ratelimit reset with backoff %s", sleepTime.String())
							time.Sleep(sleepTime)
						} else {
							err = fmt.Errorf("sleep time for abuse rate limit exceeds max sleep time (%v > %v)", sleepTime, maxSleepTime)
							resp.Body.Close()
							break
						}
					} else {
						err = fmt.Errorf("failed to parse abuse rate limit wait time %q: %w", rawTime, err)
						resp.Body.Close()
						break
					}
				} else {
					acceptedScopes := resp.Header.Get("X-Accepted-OAuth-Scopes")
					authorizedScopes := resp.Header.Get("X-OAuth-Scopes")
					if authorizedScopes == "" {
						authorizedScopes = "no"
					}

					want := sets.New[string]()
					for _, acceptedScope := range strings.Split(acceptedScopes, ",") {
						want.Insert(strings.TrimSpace(acceptedScope))
					}
					var got []string
					for _, authorizedScope := range strings.Split(authorizedScopes, ",") {
						got = append(got, strings.TrimSpace(authorizedScope))
					}
					if acceptedScopes != "" && !want.HasAny(got...) {
						err = fmt.Errorf("the account is using %s oauth scopes, please make sure you are using at least one of the following oauth scopes: %s", authorizedScopes, acceptedScopes)
					} else {
						body, _ := io.ReadAll(resp.Body)
						err = fmt.Errorf("the GitHub API request returns a 403 error: %s", string(body))
					}
					resp.Body.Close()
					break
				}
			} else if resp.StatusCode < 500 {
				// Normal, happy case.
				break
			} else {
				// Retry 500 after a break.
				klog.V(5).Infof("Retrying 5XX with backoff %s", backoff.String())
				time.Sleep(backoff)
				backoff *= 2
			}
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return resp, err
		} else {
			klog.Infof("Unhandled error in github client: %v", err)
		}
	}
	return resp, err
}

// readPaginatedResultsWithValues is an override that allows control over the query string.
func (g *github) readPaginatedResultsWithValues(path string, values url.Values, accept, org string, newObj func() interface{}, accumulate func(interface{})) error {
	return g.readPaginatedResultsWithValuesWithContext(context.Background(), path, values, accept, org, newObj, accumulate)
}

func (g *github) readPaginatedResultsWithValuesWithContext(ctx context.Context, path string, values url.Values, accept, org string, newObj func() interface{}, accumulate func(interface{})) error {
	pagedPath := path
	if len(values) > 0 {
		pagedPath += "?" + values.Encode()
	}
	for {
		resp, err := g.requestRetryWithContext(ctx, http.MethodGet, pagedPath, accept, org, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("return code not 2XX: %s", resp.Status)
		}

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		obj := newObj()
		if err := json.Unmarshal(b, obj); err != nil {
			return err
		}

		accumulate(obj)

		link := parseLinks(resp.Header.Get("Link"))["next"]
		if link == "" {
			break
		}

		// Example for github.com:
		// * c.bases[0]: api.github.com
		// * initial call: api.github.com/repos/kubernetes/kubernetes/pulls?per_page=100
		// * next: api.github.com/repositories/22/pulls?per_page=100&page=2
		// * in this case prefix will be empty and we're just calling the path returned by next
		// Example for github enterprise:
		// * c.bases[0]: <ghe-url>/api/v3
		// * initial call: <ghe-url>/api/v3/repos/kubernetes/kubernetes/pulls?per_page=100
		// * next: <ghe-url>/api/v3/repositories/22/pulls?per_page=100&page=2
		// * in this case prefix will be "/api/v3" and we will strip the prefix. If we don't do that,
		//   the next call will go to <ghe-url>/api/v3/api/v3/repositories/22/pulls?per_page=100&page=2
		prefix := strings.TrimSuffix(resp.Request.URL.RequestURI(), pagedPath)

		u, err := url.Parse(link)
		if err != nil {
			return fmt.Errorf("failed to parse 'next' link: %w", err)
		}
		pagedPath = strings.TrimPrefix(u.RequestURI(), prefix)
	}
	return nil
}

var lre = regexp.MustCompile(`<([^>]*)>; *rel="([^"]*)"`)

// Parse Link headers, returning a map from Rel to URL.
// Only understands the URI and "rel" parameter. Very limited.
// See https://tools.ietf.org/html/rfc5988#section-5
func parseLinks(h string) map[string]string {
	links := map[string]string{}
	for _, m := range lre.FindAllStringSubmatch(h, 10) {
		if len(m) != 3 {
			continue
		}
		links[m[2]] = m[1]
	}
	return links
}

type RepositoryCommit struct {
	SHA     string        `json:"sha,omitempty"`
	Commit  GHGitCommit   `json:"commit,omitempty"`
	Parents []GHGitCommit `json:"parents,omitempty"`
}

// GHGitCommit represents a GitHub commit.
type GHGitCommit struct {
	SHA       string       `json:"sha,omitempty"`
	Committer CommitAuthor `json:"committer,omitempty"`
	Message   string       `json:"message,omitempty"`
}

type CommitAuthor struct {
	Date time.Time `json:"date,omitempty"`
}

type ComparedCommits struct {
	Commits []RepositoryCommit `json:"commits,omitempty"`
}
