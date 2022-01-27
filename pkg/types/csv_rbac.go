package types

import (
	"fmt"
	"sort"

	rbac "k8s.io/api/rbac/v1"
)

type CSVPermissions struct {
	ClusterPermissions []Permissions `json:"clusterPermissions"`
	Permissions        []Permissions `json:"permissions"`
}

type Permissions struct {
	ServiceAccountName string
	Rules              []Rule
}

type Rule struct {
	rbac.PolicyRule
	name string // Used in tests
}

type RuleFilter struct {
	PermissionType           permissionType
	ApiGroupFilterObj        *FilterObj
	ResourcesFilterObj       *FilterObj
	VerbsFilterObj           *FilterObj
	ResourceNamesFilterObj   *FilterObj
	NonResourceURLsFilterObj *FilterObj
}

type FilterObj struct {
	Args         []string
	OperatorName operator
}

type Filter interface {
	filter(*rbac.PolicyRule, *FilterObj) *rbac.PolicyRule
}

type operator string

var (
	InOperator           operator = "IN"
	NotInOperator        operator = "NOT_IN"
	EqualsOperator       operator = "EQUAL"
	NotEqualOperator     operator = "NOT_EQUAL"
	ExistsOperator       operator = "EXISTS"
	DoesNotExistOperator operator = "DOES_NOT_EXIST"
	AnyOperator          operator = "ANY"
)

type permissionType string

var (
	AllPermissionType        permissionType = "all"
	NameSpacedPermissionType permissionType = "namespaced"
	ClusterPermissionType    permissionType = "clusterScoped"
)

type apiGroupFilter struct{}

func (f apiGroupFilter) filter(rule *rbac.PolicyRule, filterObj *FilterObj) *rbac.PolicyRule {
	concernedRuleAttrs := rule.APIGroups
	if eval(concernedRuleAttrs, filterObj) {
		return rule
	}
	return nil
}

type resourcesFilter struct{}

func (f resourcesFilter) filter(rule *rbac.PolicyRule, filterObj *FilterObj) *rbac.PolicyRule {
	concernedRuleAttrs := rule.Resources
	if eval(concernedRuleAttrs, filterObj) {
		return rule
	}
	return nil
}

type resourceNamesFilter struct{}

func (f resourceNamesFilter) filter(rule *rbac.PolicyRule, filterObj *FilterObj) *rbac.PolicyRule {
	concernedRuleAttrs := rule.ResourceNames
	if eval(concernedRuleAttrs, filterObj) {
		return rule
	}
	return nil
}

type verbsFilter struct{}

func (f verbsFilter) filter(rule *rbac.PolicyRule, filterObj *FilterObj) *rbac.PolicyRule {
	concernedRuleAttrs := rule.Verbs
	if eval(concernedRuleAttrs, filterObj) {
		return rule
	}
	return nil
}

type nonResourceURLsFilter struct{}

func (f nonResourceURLsFilter) filter(rule *rbac.PolicyRule, filterObj *FilterObj) *rbac.PolicyRule {
	concernedRuleAttrs := rule.NonResourceURLs
	if eval(concernedRuleAttrs, filterObj) {
		return rule
	}
	return nil
}

// Returns the list of rules matching the filtering conditions
func (cp CSVPermissions) FilterRules(ruleFilter RuleFilter) []Rule {
	concernedPermissionRules := func() []Permissions {
		switch ruleFilter.PermissionType {
		case AllPermissionType:
			res := make([]Permissions, 0)
			res = append(res, cp.ClusterPermissions...)
			res = append(res, cp.Permissions...)
			return res
		case NameSpacedPermissionType:
			return cp.Permissions
		case ClusterPermissionType:
			return cp.ClusterPermissions
		default:
			return []Permissions{}
		}
	}()
	filteredRules := make([]Rule, 0)
	for _, permissionRule := range concernedPermissionRules {
		for _, rule := range permissionRule.Rules {
			res := runFilters(getAllAttributeFilters(), &rule.PolicyRule, ruleFilter)
			if res != nil {
				filteredRules = append(filteredRules, rule)
			}
		}
	}

	return filteredRules
}

func runFilters(filters []Filter, rule *rbac.PolicyRule, ruleFilter RuleFilter) *rbac.PolicyRule {
	if len(filters) == 0 || rule == nil {
		return rule
	}

	for _, filter := range filters {
		filterObj := getConcernedFilterObj(filter, ruleFilter)
		res := filter.filter(rule, filterObj)
		if res == nil {
			return nil
		}
	}
	return rule
}

func getConcernedFilterObj(filter Filter, ruleFilter RuleFilter) *FilterObj {
	switch filter.(type) {
	case apiGroupFilter:
		return ruleFilter.ApiGroupFilterObj
	case resourcesFilter:
		return ruleFilter.ResourcesFilterObj
	case verbsFilter:
		return ruleFilter.VerbsFilterObj
	case resourceNamesFilter:
		return ruleFilter.ResourceNamesFilterObj
	case nonResourceURLsFilter:
		return ruleFilter.NonResourceURLsFilterObj
	default:
		panic("runFilters: Unknown filter type")
	}
}

func getAllAttributeFilters() []Filter {
	return []Filter{
		apiGroupFilter{},
		resourcesFilter{},
		verbsFilter{},
		resourceNamesFilter{},
		nonResourceURLsFilter{},
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

func eval(ruleArgs []string, filterObj *FilterObj) bool {
	if filterObj == nil {
		return true
	}
	switch filterObj.OperatorName {
	case InOperator:
		return includes(ruleArgs, filterObj.Args)
	case NotInOperator:
		return !includes(ruleArgs, filterObj.Args)
	case EqualsOperator:
		return equal(ruleArgs, filterObj.Args)
	case NotEqualOperator:
		return !equal(ruleArgs, filterObj.Args)
	case ExistsOperator:
		return len(ruleArgs) > 0
	case DoesNotExistOperator:
		return len(ruleArgs) == 0
	case AnyOperator:
		return any(ruleArgs, filterObj.Args)
	default:
		panic(fmt.Sprintf("eval: Unsupported operator %s", filterObj.OperatorName))
	}
}
