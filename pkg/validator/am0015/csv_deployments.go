package am0015

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mt-sre/addon-metadata-operator/internal/kube"
	"github.com/mt-sre/addon-metadata-operator/pkg/types"
	"github.com/mt-sre/addon-metadata-operator/pkg/validator"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/pkg/registry"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
)

func init() {
	validator.Register(NewCSVDeployment)
}

const (
	code = 15
	name = "csv_deployments"
	desc = "Ensure all deployment in CSV must have valid resource requests, livenessprobe and readinessprobe"
)

func NewCSVDeployment(deps validator.Dependencies) (validator.Validator, error) {
	base, err := validator.NewBase(
		code,
		validator.BaseName(name),
		validator.BaseDesc(desc),
	)
	if err != nil {
		return nil, err
	}

	return &CSVDeployment{
		Base: base,
	}, nil
}

type CSVDeployment struct {
	*validator.Base
}

type Spec struct {
	InstallStrategy operatorv1alpha1.NamedInstallStrategy `json:"install"`
}

func (c *CSVDeployment) Run(ctx context.Context, mb types.MetaBundle) validator.Result {
	var msg []string
	var spec Spec
	bundle, err := getLatestBundle(mb.Bundles)
	if err != nil {
		c.Fail("Error while checking bundles")
	}

	csv, err := bundle.ClusterServiceVersion()
	if err != nil {
		c.Error(err)
	}

	if err := json.Unmarshal(csv.Spec, &spec); err != nil {
		c.Error(err)
	}

	for _, deploymentSpec := range spec.InstallStrategy.StrategySpec.DeploymentSpecs {
		deployment := appsv1.Deployment{Spec: deploymentSpec.Spec}

		if hasLivenessProbe := kube.HasLivenessProbes(deployment); !hasLivenessProbe.Success {
			msg = append(msg, hasLivenessProbe.Reasons...)
		}

		if hasReadinessProbe := kube.HasReadinessProbes(deployment); !hasReadinessProbe.Success {
			msg = append(msg, hasReadinessProbe.Reasons...)
		}

		if hasCPUresource := kube.HasCPUResourceRequirements(deployment); !hasCPUresource.Success {
			msg = append(msg, hasCPUresource.Reasons...)
		}

		if hasMemory := kube.HasMemoryResourceRequirements(deployment); !hasMemory.Success {
			msg = append(msg, hasMemory.Reasons...)
		}
	}

	if len(msg) > 0 {
		return c.Fail(msg...)
	}
	return c.Success()
}

func getLatestBundle(bundles []*registry.Bundle) (*registry.Bundle, error) {
	if len(bundles) == 1 {
		return bundles[0], nil
	}

	latest := bundles[0]
	for _, bundle := range bundles[1:] {
		currVersion, err := getVersion(bundle)
		if err != nil {
			return nil, err
		}
		currLatestVersion, err := getVersion(latest)
		if err != nil {
			return nil, err
		}

		res := semver.Compare(currVersion, currLatestVersion)
		// If currVersion is greater than currLatestVersion
		if res == 1 {
			latest = bundle
		}
	}
	return latest, nil
}

func getVersion(bundle *registry.Bundle) (string, error) {
	csv, err := bundle.ClusterServiceVersion()
	if err != nil {
		return "", err
	}

	version, err := csv.GetVersion()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("v%s", version), nil
}
