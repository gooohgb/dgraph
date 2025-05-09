/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package upgrade

import (
	"encoding/json"
	"fmt"

	"github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/hypermodeinc/dgraph/v25/x"
)

const (
	queryACLGroupsBefore_v20_03_0 = `
		{
			rules(func: type(Group)) @filter(has(dgraph.group.acl)) {
				uid
				dgraph.group.acl
			}
		}
	`
)

type group struct {
	UID string `json:"uid"`
	ACL string `json:"dgraph.group.acl,omitempty"`
}

type rule struct {
	Predicate  string `json:"predicate,omitempty"`
	Permission int    `json:"perm,omitempty"`
}

type rules []rule

func upgradeACLRules() error {
	dg, cb := x.GetDgraphClient(Upgrade.Conf, true)
	defer cb()

	data := make(map[string][]group)
	if err := getQueryResult(dg, queryACLGroupsBefore_v20_03_0, &data); err != nil {
		return fmt.Errorf("error querying old ACL rules: %w", err)
	}

	groups, ok := data["rules"]
	if !ok {
		return fmt.Errorf("unable to parse ACLs: %v", data)
	}

	counter := 1
	var nquads []*api.NQuad
	for _, group := range groups {
		if group.ACL == "" {
			continue
		}

		var rs rules
		if err := json.Unmarshal([]byte(group.ACL), &rs); err != nil {
			return fmt.Errorf("unable to unmarshal ACL: %v :: %w", group.ACL, err)
		}

		for _, r := range rs {
			newRuleStr := fmt.Sprintf("_:newrule%d", counter)
			nquads = append(nquads, []*api.NQuad{
				// the name of the type was Rule in v20.03.0
				getTypeNquad(newRuleStr, "Rule"),
				{
					Subject:   newRuleStr,
					Predicate: "dgraph.rule.predicate",
					ObjectValue: &api.Value{
						Val: &api.Value_StrVal{StrVal: r.Predicate},
					},
				},
				{
					Subject:   newRuleStr,
					Predicate: "dgraph.rule.permission",
					ObjectValue: &api.Value{
						Val: &api.Value_IntVal{IntVal: int64(r.Permission)},
					},
				},
				{
					Subject:   group.UID,
					Predicate: "dgraph.acl.rule",
					ObjectId:  newRuleStr,
				},
			}...)

			counter++
		}
	}

	// Nothing to do.
	if len(nquads) == 0 {
		fmt.Println("nothing to do: no old rules found in the cluster")
		return nil
	}

	if err := mutateWithClient(dg, &api.Mutation{Set: nquads}); err != nil {
		return fmt.Errorf("error upgrading ACL rules: %w", err)
	}
	fmt.Println("Successfully upgraded ACL rules.")

	deleteOld := Upgrade.Conf.GetBool("deleteOld")
	if deleteOld {
		err := alterWithClient(dg, &api.Operation{
			DropOp:    api.Operation_ATTR,
			DropValue: "dgraph.group.acl",
		})
		if err != nil {
			return fmt.Errorf("error deleting old acl predicates: %w", err)
		}
		fmt.Println("Successfully deleted old rules.")
	}

	return nil
}
