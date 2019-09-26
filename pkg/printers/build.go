package printers

import (
	"fmt"
	"regexp"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kprinters "k8s.io/kubernetes/pkg/printers"

	buildv1 "github.com/openshift/api/build/v1"
	buildhelpers "github.com/openshift/oc/pkg/helpers/build"
)

func AddBuildOpenShiftHandlers(h kprinters.PrintHandler) {
	buildColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Type", Type: "string", Description: "Describes a particular way of performing a build."},
		{Name: "From", Type: "string", Description: buildv1.CommonSpec{}.SwaggerDoc()["source"]},
		{Name: "Status", Type: "string", Description: buildv1.BuildStatus{}.SwaggerDoc()["phase"]},
		{Name: "Started", Type: "string", Description: buildv1.BuildStatus{}.SwaggerDoc()["startTimestamp"]},
		{Name: "Duration", Type: "string", Description: buildv1.BuildStatus{}.SwaggerDoc()["duration"]},
	}
	if err := h.TableHandler(buildColumnDefinitions, printBuildList); err != nil {
		panic(err)
	}
	if err := h.TableHandler(buildColumnDefinitions, printBuild); err != nil {
		panic(err)
	}

	buildConfigColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Type", Type: "string", Description: "Describes a particular way of performing a build."},
		{Name: "From", Type: "string", Description: buildv1.CommonSpec{}.SwaggerDoc()["source"]},
		{Name: "Latest", Type: "string", Description: buildv1.BuildConfigStatus{}.SwaggerDoc()["lastVersion"]},
	}
	if err := h.TableHandler(buildConfigColumnDefinitions, printBuildConfigList); err != nil {
		panic(err)
	}
	if err := h.TableHandler(buildConfigColumnDefinitions, printBuildConfig); err != nil {
		panic(err)
	}
}

func printBuild(build *buildv1.Build, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: build},
	}

	name := formatResourceName(options.Kind, build.Name, options.WithKind)
	var created string
	if build.Status.StartTimestamp != nil {
		created = fmt.Sprintf("%s ago", formatRelativeTime(build.Status.StartTimestamp.Time))
	}
	var duration string
	if build.Status.Duration > 0 {
		duration = build.Status.Duration.String()
	}
	from := describeSourceShort(build.Spec.CommonSpec)
	status := string(build.Status.Phase)
	if len(build.Status.Reason) > 0 {
		status = fmt.Sprintf("%s (%s)", status, build.Status.Reason)
	}

	row.Cells = append(row.Cells, name, buildhelpers.StrategyType(build.Spec.Strategy),
		from, status, created, duration)

	return []metav1.TableRow{row}, nil
}

func printBuildList(list *buildv1.BuildList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	sort.Sort(buildhelpers.BuildSliceByCreationTimestamp(list.Items))
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printBuild(&list.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printBuildConfig(bc *buildv1.BuildConfig, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: bc},
	}

	name := formatResourceName(options.Kind, bc.Name, options.WithKind)
	from := describeSourceShort(bc.Spec.CommonSpec)

	if bc.Spec.Strategy.CustomStrategy != nil {
		row.Cells = append(row.Cells, name, buildhelpers.StrategyType(bc.Spec.Strategy),
			bc.Spec.Strategy.CustomStrategy.From.Name, bc.Status.LastVersion)
	} else {
		row.Cells = append(row.Cells, name, buildhelpers.StrategyType(bc.Spec.Strategy), from,
			bc.Status.LastVersion)
	}

	return []metav1.TableRow{row}, nil

}

func printBuildConfigList(list *buildv1.BuildConfigList, options kprinters.PrintOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printBuildConfig(&list.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func describeSourceShort(spec buildv1.CommonSpec) string {
	var from string
	switch source := spec.Source; {
	case source.Binary != nil:
		from = "Binary"
		if rev := describeSourceGitRevision(spec); len(rev) != 0 {
			from = fmt.Sprintf("%s@%s", from, rev)
		}
	case source.Dockerfile != nil && source.Git != nil:
		from = "Dockerfile,Git"
		if rev := describeSourceGitRevision(spec); len(rev) != 0 {
			from = fmt.Sprintf("%s@%s", from, rev)
		}
	case source.Dockerfile != nil:
		from = "Dockerfile"
	case source.Git != nil:
		from = "Git"
		if rev := describeSourceGitRevision(spec); len(rev) != 0 {
			from = fmt.Sprintf("%s@%s", from, rev)
		}
	default:
		from = buildSourceType(source)
	}
	return from
}

func buildSourceType(source buildv1.BuildSource) string {
	var sourceType string
	if source.Git != nil {
		sourceType = "Git"
	}
	if source.Dockerfile != nil {
		if len(sourceType) != 0 {
			sourceType = sourceType + ","
		}
		sourceType = sourceType + "Dockerfile"
	}
	if source.Binary != nil {
		if len(sourceType) != 0 {
			sourceType = sourceType + ","
		}
		sourceType = sourceType + "Binary"
	}
	return sourceType
}

var nonCommitRev = regexp.MustCompile("[^a-fA-F0-9]")

func describeSourceGitRevision(spec buildv1.CommonSpec) string {
	var rev string
	if spec.Revision != nil && spec.Revision.Git != nil {
		rev = spec.Revision.Git.Commit
	}
	if len(rev) == 0 && spec.Source.Git != nil {
		rev = spec.Source.Git.Ref
	}
	// if this appears to be a full Git commit hash, shorten it to 7 characters for brevity
	if !nonCommitRev.MatchString(rev) && len(rev) > 20 {
		rev = rev[:7]
	}
	return rev
}
