package am0003

import (
	"context"
	"fmt"
	"strings"

	"github.com/mt-sre/addon-metadata-operator/pkg/types"
	"github.com/mt-sre/addon-metadata-operator/pkg/utils"
	"github.com/mt-sre/addon-metadata-operator/pkg/validator"
	"github.com/operator-framework/operator-registry/pkg/registry"
	"golang.org/x/mod/semver"
)

const (
	code = 3
	name = "operator_name"
	desc = "Validate the operatorName matches csv.Name, csv.Replaces and bundle package annotation."
)

func init() {
	validator.Register(NewOperatorName)
}

func NewOperatorName(deps validator.Dependencies) (validator.Validator, error) {
	base, err := validator.NewBase(
		code,
		validator.BaseName(name),
		validator.BaseDesc(desc),
	)
	if err != nil {
		return nil, err
	}
	return &OperatorName{
		Base: base,
	}, nil
}

type OperatorName struct {
	*validator.Base
}

type bundleDataAM0003 struct {
	nameVersion       string
	csvName           string
	csvReplaces       string
	pkgNameAnnotation string
}

func newBundleData(bundle *registry.Bundle) (bundleDataAM0003, error) {
	var bundleData bundleDataAM0003
	nameVersion, err := utils.GetBundleNameVersion(*bundle)
	if err != nil {
		return bundleData, fmt.Errorf("could not get bundle name and version: %w", err)
	}

	csv, err := bundle.ClusterServiceVersion()
	if err != nil {
		return bundleData, fmt.Errorf("could not get csv for bundle '%s': %w", nameVersion, err)
	}

	replaces, err := csv.GetReplaces()
	if err != nil {
		return bundleData, fmt.Errorf("could not get csv.Replaces for bundle '%s': %w", nameVersion, err)
	}

	bundleData.csvName = csv.Name
	bundleData.csvReplaces = replaces
	bundleData.nameVersion = nameVersion
	bundleData.pkgNameAnnotation = bundle.Annotations.PackageName

	return bundleData, nil
}

func checkCSVNameOrReplaces(csvField, operatorName string) string {
	parts := strings.SplitN(csvField, ".", 2)
	if len(parts) != 2 {
		return fmt.Sprintf("could not split '%s' in two parts.", csvField)
	}
	bundleOperatorName := parts[0]
	bundleVersion := parts[1]

	if bundleOperatorName != operatorName {
		return fmt.Sprintf("invalid operatorName for '%s', should match '%s'", csvField, operatorName)
	}
	if !isValidSemver(bundleVersion) {
		return fmt.Sprintf("invalid semver '%s'", csvField)
	}
	return ""
}

func isValidSemver(version string) bool {
	if !strings.HasPrefix(version, "v") {
		version = fmt.Sprintf("v%s", version)
	}
	return semver.IsValid(version)
}

func (o *OperatorName) Run(ctx context.Context, mb types.MetaBundle) validator.Result {
	var failureMsgs []string
	operatorName, bundles := mb.AddonMeta.OperatorName, mb.Bundles
	for _, bundle := range bundles {
		bundleData, err := newBundleData(&bundle)
		if err != nil {
			return o.Error(err)
		}

		if msg := checkCSVNameOrReplaces(bundleData.csvName, operatorName); msg != "" {
			msg := fmt.Sprintf("bundle %s failed validation on csv.Name: %s.", bundleData.nameVersion, msg)
			failureMsgs = append(failureMsgs, msg)
		}

		if bundleData.csvReplaces != "" {
			if msg := checkCSVNameOrReplaces(bundleData.csvReplaces, operatorName); msg != "" {
				msg := fmt.Sprintf("bundle '%s' failed validation on csv.Replaces: %s", bundleData.nameVersion, msg)
				failureMsgs = append(failureMsgs, msg)
			}
		}

		if bundleData.pkgNameAnnotation != operatorName {
			msg := fmt.Sprintf("bundle '%s' package annotation does not match operatorName '%s'", bundleData.nameVersion, operatorName)
			failureMsgs = append(failureMsgs, msg)
		}
	}
	if len(failureMsgs) > 0 {
		return o.Fail(strings.Join(failureMsgs, ", "))
	}
	return o.Success()
}
