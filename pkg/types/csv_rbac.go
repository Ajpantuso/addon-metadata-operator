package types

import (
	"fmt"
	"sort"

	rbac "k8s.io/api/rbac/v1"
)

type CSVPermissions struct {
	ClusterPermissions []Permission `json:"clusterPermissions"`
	Permissions        []Permission `json:"permissions"`
}

// Returns the list of rules matching the filtering conditions
func (cp CSVPermissions) FilterRules(ruleFilter RuleFilter) []Rule {
	var filtered []Rule

	for _, perm := range ruleFilter.GetRelevantPermissions(cp) {
		for _, rule := range perm.Rules {
			if res := ruleFilter.Run(&rule.PolicyRule); res == nil {
				continue
			}

			filtered = append(filtered, rule)
		}
	}

	return filtered
}

type Permission struct {
	ServiceAccountName string
	Rules              []Rule
}

type Rule struct {
	rbac.PolicyRule
	name string // Used in tests
}

type permissionType string

const (
	AllPermissionType        permissionType = "all"
	NameSpacedPermissionType permissionType = "namespaced"
	ClusterPermissionType    permissionType = "clusterScoped"
)

type RuleFilter struct {
	PermissionType permissionType
	Filters        []Filter
}

func (r *RuleFilter) GetRelevantPermissions(cp CSVPermissions) []Permission {
	switch r.PermissionType {
	case AllPermissionType:
		return append(cp.ClusterPermissions, cp.Permissions...)
	case NameSpacedPermissionType:
		return cp.Permissions
	case ClusterPermissionType:
		return cp.ClusterPermissions
	default:
		return []Permission{}
	}
}

func (r *RuleFilter) Run(rule *rbac.PolicyRule) *rbac.PolicyRule {
	if len(r.Filters) == 0 || rule == nil {
		return rule
	}

	for _, f := range r.Filters {
		if res := f.Filter(rule); res != nil {
			continue
		}

		return nil
	}

	return rule
}

type FilterParams struct {
	Args         []string
	OperatorName Operator
}

type Operator string

const (
	InOperator           Operator = "IN"
	NotInOperator        Operator = "NOT_IN"
	EqualsOperator       Operator = "EQUAL"
	NotEqualOperator     Operator = "NOT_EQUAL"
	ExistsOperator       Operator = "EXISTS"
	DoesNotExistOperator Operator = "DOES_NOT_EXIST"
	AnyOperator          Operator = "ANY"
)

type Filter interface {
	Filter(*rbac.PolicyRule) *rbac.PolicyRule
}

type APIGroupFilter struct {
	Params FilterParams
}

func (f *APIGroupFilter) Filter(rule *rbac.PolicyRule) *rbac.PolicyRule {
	concernedRuleAttrs := rule.APIGroups
	if eval(concernedRuleAttrs, f.Params) {
		return rule
	}

	return nil
}

type ResourcesFilter struct {
	Params FilterParams
}

func (f *ResourcesFilter) Filter(rule *rbac.PolicyRule) *rbac.PolicyRule {
	concernedRuleAttrs := rule.Resources
	if eval(concernedRuleAttrs, f.Params) {
		return rule
	}

	return nil
}

type ResourceNamesFilter struct {
	Params FilterParams
}

func (f *ResourceNamesFilter) Filter(rule *rbac.PolicyRule) *rbac.PolicyRule {
	concernedRuleAttrs := rule.ResourceNames
	if eval(concernedRuleAttrs, f.Params) {
		return rule
	}

	return nil
}

type VerbsFilter struct {
	Params FilterParams
}

func (f *VerbsFilter) Filter(rule *rbac.PolicyRule) *rbac.PolicyRule {
	concernedRuleAttrs := rule.Verbs
	if eval(concernedRuleAttrs, f.Params) {
		return rule
	}

	return nil
}

type NonResourceURLsFilter struct {
	Params FilterParams
}

func (f *NonResourceURLsFilter) Filter(rule *rbac.PolicyRule) *rbac.PolicyRule {
	concernedRuleAttrs := rule.NonResourceURLs
	if eval(concernedRuleAttrs, f.Params) {
		return rule
	}

	return nil
}

func eval(ruleArgs []string, params FilterParams) bool {
	switch params.OperatorName {
	case InOperator:
		return includes(ruleArgs, params.Args)
	case NotInOperator:
		return !includes(ruleArgs, params.Args)
	case EqualsOperator:
		return equal(ruleArgs, params.Args)
	case NotEqualOperator:
		return !equal(ruleArgs, params.Args)
	case ExistsOperator:
		return len(ruleArgs) > 0
	case DoesNotExistOperator:
		return len(ruleArgs) == 0
	case AnyOperator:
		return any(ruleArgs, params.Args)
	default:
		panic(fmt.Sprintf("eval: Unsupported operator %s", params.OperatorName))
	}
}

func includes(items []string, itemsToBePresent []string) bool {
	itemsMap := sliceToSet(items)
	for _, item := range itemsToBePresent {
		if _, ok := itemsMap[item]; !ok {
			return false
		}
	}
	return true
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// Needed for thread safety.
	copyA := make([]string, len(a))
	copyB := make([]string, len(b))
	copy(copyA, a)
	copy(copyB, b)
	sort.Strings(copyA)
	sort.Strings(copyB)

	for index := range copyA {
		if copyA[index] != copyB[index] {
			return false
		}
	}
	return true
}

// Checks if any element in b is present in a.
func any(a, b []string) bool {
	source := sliceToSet(a)
	for _, item := range b {
		if _, ok := source[item]; ok {
			return true
		}
	}
	return false
}

func sliceToSet(items []string) map[string]struct{} {
	res := make(map[string]struct{}, len(items))
	for _, item := range items {
		res[item] = struct{}{}
	}
	return res
}
