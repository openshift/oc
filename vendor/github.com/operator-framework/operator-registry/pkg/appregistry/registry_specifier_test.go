package appregistry

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
)

func TestParseSource(t *testing.T) {
	p := registrySpecifier{}

	sourceWant := &Source{
		Endpoint:          "https://quay.io/cnr",
		RegistryNamespace: "community-operators",
		Secret: types.NamespacedName{
			Namespace: "mynamespace",
			Name:      "mysecret",
		},
	}

	input := fmt.Sprintf("%s | %s |%s /  %s", sourceWant.Endpoint,
		sourceWant.RegistryNamespace, sourceWant.Secret.Namespace, sourceWant.Secret.Name)
	sourceGot, errGot := p.ParseOne(input)

	assert.NoError(t, errGot)
	assert.Equal(t, sourceWant, sourceGot)
}

func TestParseSourceWithLeadingOrTrailingDelimiter(t *testing.T) {
	p := registrySpecifier{}

	sourceWant := &Source{
		Endpoint:          "https://quay.io/cnr",
		RegistryNamespace: "community-operators",
		Secret: types.NamespacedName{
			Namespace: "mynamespace",
			Name:      "mysecret",
		},
	}

	input := fmt.Sprintf("|%s|%s|%s/%s|", sourceWant.Endpoint,
		sourceWant.RegistryNamespace, sourceWant.Secret.Namespace, sourceWant.Secret.Name)
	sourceGot, errGot := p.ParseOne(input)

	assert.NoError(t, errGot)
	assert.Equal(t, sourceWant, sourceGot)
}

func TestParseSourceWithNoSecret(t *testing.T) {
	p := registrySpecifier{}

	sourceWant := &Source{
		Endpoint:          "https://quay.io/cnr",
		RegistryNamespace: "community-operators",
		Secret: types.NamespacedName{
			Namespace: "",
			Name:      "",
		},
	}

	input := fmt.Sprintf("%s|%s|", sourceWant.Endpoint, sourceWant.RegistryNamespace)
	sourceGot, errGot := p.ParseOne(input)

	assert.NoError(t, errGot)
	assert.Equal(t, sourceWant, sourceGot)
}

func TestParseSourcesWithError(t *testing.T) {
	sourcesWant := []*Source{
		&Source{
			Endpoint:          "https://quay.io/cnr",
			RegistryNamespace: "community-operators",
			Secret: types.NamespacedName{
				Namespace: "mynamespace",
				Name:      "mysecret",
			},
		},
		&Source{
			Endpoint:          "https://quay.io/cnr",
			RegistryNamespace: "redhat-operators",
			Secret: types.NamespacedName{
				Namespace: "",
				Name:      "",
			},
		},
	}

	input := []string{
		fmt.Sprintf("|%s|%s|%s/%s|", sourcesWant[0].Endpoint, sourcesWant[0].RegistryNamespace, sourcesWant[0].Secret.Namespace, sourcesWant[0].Secret.Name),
		fmt.Sprintf("|%s|%s|", sourcesWant[1].Endpoint, sourcesWant[1].RegistryNamespace),
		fmt.Sprintf("abc|xyz|123|pqr"),
	}

	p := registrySpecifier{}
	sourcesGot, errGot := p.Parse(input)

	assert.Error(t, errGot)
	assert.ElementsMatch(t, sourcesWant, sourcesGot)
}
