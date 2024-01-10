package status

import (
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type scopeType string

const (
	scopeControlPlane scopeType = "ControlPlane"
	scopeWorkerPool   scopeType = "WorkerPool"
)

type allowedScopeKind string

const (
	clusterOperator allowedScopeKind = "ClusterOperator"
)

type scopeResource struct {
	kind      allowedScopeKind
	namespace string
	name      string
}

type updateInsightScope struct {
	scopeType scopeType
	resources []scopeResource
}

type impactLevel uint32

const (
	// infoImpactLevel should be used for insights that are strictly informational or even positive (things go well or
	// something recently healed)
	infoImpactLevel impactLevel = iota
	// warningImpactLevel should be used for insights that explain a minor or transient problem. Anything that requires
	// admin attention or manual action should not be a warning but at least an error.
	warningImpactLevel
	// errorImpactLevel should be used for insights that inform about a problem that requires admin attention. Insights of
	// level error and higher should be as actionable as possible, and should be accompanied by links to documentation,
	// KB articles or other resources that help the admin to resolve the problem.
	errorImpactLevel
	// criticalInfoLevel should be used rarely, for insights that inform about a severe problem, threatening with data
	// loss, destroyed cluster or other catastrophic consequences. Insights of this level should be accompanied by
	// links to documentation, KB articles or other resources that help the admin to resolve the problem, or at least
	// prevent the severe consequences from happening.
	criticalInfoLevel
)

func (l impactLevel) String() string {
	switch l {
	case infoImpactLevel:
		return "Info"
	case warningImpactLevel:
		return "Warning"
	case errorImpactLevel:
		return "Error"
	case criticalInfoLevel:
		return "Critical"
	default:
		return "Unknown"
	}
}

type impactType string

const (
	noneImpactType                    impactType = "None"
	apiAvailabilityImpactType         impactType = "API Availability"
	clusterCapacityImpactType         impactType = "Cluster Capacity"
	applicationAvailabilityImpactType impactType = "Application Availability"
	applicationOutageImpactType       impactType = "Application Outage"
	dataLossImpactType                impactType = "Data Loss"
	updateSpeedImpactType             impactType = "Update Speed"
	updateStalledImpactType           impactType = "Update Stalled"
)

type updateInsightImpact struct {
	level      impactLevel
	impactType impactType
	summary    string
}

type updateInsight struct {
	startedAt time.Time
	scope     updateInsightScope
	impact    updateInsightImpact
}

type updateHealthData struct {
	evaluatedAt time.Time
	insights    []updateInsight
}

func assessUpdateInsights(insights []updateInsight, upgradingFor time.Duration, evaluatedAt time.Time) updateHealthData {
	sorted := make([]updateInsight, 0, len(insights))
	sorted = append(sorted, insights...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].startedAt.After(sorted[j].startedAt)
	})
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].impact.level > sorted[j].impact.level
	})

	if len(sorted) == 0 {
		sorted = append(sorted, updateInsight{
			startedAt: evaluatedAt.Add(-upgradingFor),
			impact: updateInsightImpact{
				level:      infoImpactLevel,
				impactType: noneImpactType,
				summary:    "Upgrade is proceeding well",
			},
		})
	}

	return updateHealthData{
		evaluatedAt: evaluatedAt,
		insights:    sorted,
	}
}

func shortDuration(d time.Duration) string {
	orig := d.String()
	switch {
	case orig == "0h0m0s":
		return "now"
	case strings.HasSuffix(orig, "0m0s"):
		return orig[:len(orig)-4]
	case strings.HasSuffix(orig, "m0s"):
		return orig[:len(orig)-2]
	case strings.HasSuffix(orig, "h0m"):
		return orig[:len(orig)-2]
	}
	return orig
}

func (i *updateHealthData) Write(w io.Writer) error {
	_, _ = w.Write([]byte("= Update Health =\n"))
	tabw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	_, _ = tabw.Write([]byte("SINCE\tLEVEL\tIMPACT\tMESSAGE\n"))
	for _, insight := range i.insights {
		_, _ = tabw.Write([]byte(shortDuration(i.evaluatedAt.Sub(insight.startedAt).Truncate(time.Second)) + "\t"))
		_, _ = tabw.Write([]byte(insight.impact.level.String() + "\t"))
		_, _ = tabw.Write([]byte(insight.impact.impactType + "\t"))
		_, _ = tabw.Write([]byte(insight.impact.summary + "\n"))
	}
	tabw.Flush()

	return nil
}
