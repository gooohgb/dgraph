//go:build integration

/*
 * SPDX-FileCopyrightText: © Hypermode Inc. <hello@hypermode.com>
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dgraph-io/dgo/v250/protos/api"
	"github.com/hypermodeinc/dgraph/v25/dgraphapi"
	"github.com/hypermodeinc/dgraph/v25/dgraphtest"
	"github.com/hypermodeinc/dgraph/v25/x"
)

func TestVectorIncrBackupRestore(t *testing.T) {
	conf := dgraphtest.NewClusterConfig().WithNumAlphas(1).WithNumZeros(1).WithReplicas(1).WithACL(time.Hour)
	c, err := dgraphtest.NewLocalCluster(conf)
	require.NoError(t, err)
	defer func() { c.Cleanup(t.Failed()) }()
	require.NoError(t, c.Start())

	gc, cleanup, err := c.Client()
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, gc.LoginIntoNamespace(context.Background(),
		dgraphapi.DefaultUser, dgraphapi.DefaultPassword, x.RootNamespace))

	hc, err := c.HTTPClient()
	require.NoError(t, err)
	require.NoError(t, hc.LoginIntoNamespace(dgraphapi.DefaultUser,
		dgraphapi.DefaultPassword, x.RootNamespace))

	require.NoError(t, gc.SetupSchema(testSchema))

	numVectors := 500
	pred := "project_description_v"
	allVectors := make([][][]float32, 0, 5)
	allRdfs := make([]string, 0, 5)
	for i := 1; i <= 5; i++ {
		var rdfs string
		var vectors [][]float32
		rdfs, vectors = dgraphapi.GenerateRandomVectors(numVectors*(i-1), numVectors*i, 1, pred)
		allVectors = append(allVectors, vectors)
		allRdfs = append(allRdfs, rdfs)
		mu := &api.Mutation{SetNquads: []byte(rdfs), CommitNow: true}
		_, err := gc.Mutate(mu)
		require.NoError(t, err)

		t.Logf("taking backup #%v\n", i)
		require.NoError(t, hc.Backup(c, i == 1, dgraphtest.DefaultBackupDir))
	}

	for i := 1; i <= 5; i++ {
		t.Logf("restoring backup #%v\n", i)

		incrFrom := i - 1
		require.NoError(t, hc.Restore(c, dgraphtest.DefaultBackupDir, "", incrFrom, i))
		require.NoError(t, dgraphapi.WaitForRestore(c))
		query := `{
			vector(func: has(project_description_v)) {
				   count(uid)
				}
		}`
		result, err := gc.Query(query)
		require.NoError(t, err)

		require.JSONEq(t, fmt.Sprintf(`{"vector":[{"count":%v}]}`, numVectors*i), string(result.GetJson()))
		var allSpredVec [][]float32
		for i, vecArr := range allVectors {
			if i <= i {
				allSpredVec = append(allSpredVec, vecArr...)
			}
		}
		for p, vector := range allVectors[i-1] {
			triple := strings.Split(allRdfs[i-1], "\n")[p]
			uid := strings.Split(triple, " ")[0]
			queriedVector, err := gc.QuerySingleVectorsUsingUid(uid, pred)
			require.NoError(t, err)

			require.Equal(t, allVectors[i-1][p], queriedVector[0])

			similarVectors, err := gc.QueryMultipleVectorsUsingSimilarTo(vector, pred, numVectors)
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(similarVectors), 10)
			for _, similarVector := range similarVectors {
				require.Contains(t, allSpredVec, similarVector)
			}
		}
	}
}

func TestVectorBackupRestore(t *testing.T) {
	conf := dgraphtest.NewClusterConfig().WithNumAlphas(1).WithNumZeros(1).WithReplicas(1).WithACL(time.Hour)
	c, err := dgraphtest.NewLocalCluster(conf)
	require.NoError(t, err)
	defer func() { c.Cleanup(t.Failed()) }()
	require.NoError(t, c.Start())

	gc, cleanup, err := c.Client()
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, gc.LoginIntoNamespace(context.Background(),
		dgraphapi.DefaultUser, dgraphapi.DefaultPassword, x.RootNamespace))

	hc, err := c.HTTPClient()
	require.NoError(t, err)
	require.NoError(t, hc.LoginIntoNamespace(dgraphapi.DefaultUser,
		dgraphapi.DefaultPassword, x.RootNamespace))

	require.NoError(t, gc.SetupSchema(testSchema))

	numVectors := 1000
	pred := "project_description_v"
	rdfs, vectors := dgraphapi.GenerateRandomVectors(0, numVectors, 10, pred)

	mu := &api.Mutation{SetNquads: []byte(rdfs), CommitNow: true}
	_, err = gc.Mutate(mu)
	require.NoError(t, err)

	t.Log("taking backup \n")
	require.NoError(t, hc.Backup(c, false, dgraphtest.DefaultBackupDir))

	t.Log("restoring backup \n")
	require.NoError(t, hc.Restore(c, dgraphtest.DefaultBackupDir, "", 0, 0))
	require.NoError(t, dgraphapi.WaitForRestore(c))

	testVectorQuery(t, gc, vectors, rdfs, pred, numVectors)
}

func TestVectorBackupRestoreDropIndex(t *testing.T) {
	// setup cluster
	conf := dgraphtest.NewClusterConfig().WithNumAlphas(1).WithNumZeros(1).WithReplicas(1).WithACL(time.Hour)
	c, err := dgraphtest.NewLocalCluster(conf)
	require.NoError(t, err)
	defer func() { c.Cleanup(t.Failed()) }()
	require.NoError(t, c.Start())

	gc, cleanup, err := c.Client()
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, gc.LoginIntoNamespace(context.Background(),
		dgraphapi.DefaultUser, dgraphapi.DefaultPassword, x.RootNamespace))

	hc, err := c.HTTPClient()
	require.NoError(t, err)
	require.NoError(t, hc.LoginIntoNamespace(dgraphapi.DefaultUser,
		dgraphapi.DefaultPassword, x.RootNamespace))

	// add vector predicate + index
	require.NoError(t, gc.SetupSchema(testSchema))
	// add data to the vector predicate
	numVectors := 3
	pred := "project_description_v"
	rdfs, vectors := dgraphapi.GenerateRandomVectors(0, numVectors, 1, pred)
	mu := &api.Mutation{SetNquads: []byte(rdfs), CommitNow: true}
	_, err = gc.Mutate(mu)
	require.NoError(t, err)

	t.Log("taking full backup \n")
	require.NoError(t, hc.Backup(c, false, dgraphtest.DefaultBackupDir))

	// drop index
	require.NoError(t, gc.SetupSchema(testSchemaWithoutIndex))

	// add more data to the vector predicate
	rdfs, vectors2 := dgraphapi.GenerateRandomVectors(3, numVectors+3, 1, pred)
	mu = &api.Mutation{SetNquads: []byte(rdfs), CommitNow: true}
	_, err = gc.Mutate(mu)
	require.NoError(t, err)

	// delete some entries
	mu = &api.Mutation{DelNquads: []byte(strings.Split(rdfs, "\n")[1]), CommitNow: true}
	_, err = gc.Mutate(mu)
	require.NoError(t, err)

	vectors2 = slices.Delete(vectors2, 1, 2)

	mu = &api.Mutation{DelNquads: []byte(strings.Split(rdfs, "\n")[0]), CommitNow: true}
	_, err = gc.Mutate(mu)
	require.NoError(t, err)
	vectors2 = slices.Delete(vectors2, 0, 1)

	t.Log("taking first incr backup \n")
	require.NoError(t, hc.Backup(c, false, dgraphtest.DefaultBackupDir))

	// add index
	require.NoError(t, gc.SetupSchema(testSchema))

	t.Log("taking second incr backup \n")
	require.NoError(t, hc.Backup(c, false, dgraphtest.DefaultBackupDir))

	// restore backup
	t.Log("restoring backup \n")
	require.NoError(t, hc.Restore(c, dgraphtest.DefaultBackupDir, "", 0, 0))
	require.NoError(t, dgraphapi.WaitForRestore(c))

	query := ` {
		vectors(func: has(project_description_v)) {
			   count(uid)
			 }
		}`
	resp, err := gc.Query(query)
	require.NoError(t, err)
	require.JSONEq(t, `{"vectors":[{"count":4}]}`, string(resp.GetJson()))

	require.NoError(t, err)
	allVec := append(vectors, vectors2...)

	for _, vector := range allVec {

		similarVectors, err := gc.QueryMultipleVectorsUsingSimilarTo(vector, pred, 4)
		require.NoError(t, err)
		for _, similarVector := range similarVectors {
			require.Contains(t, allVec, similarVector)
		}
	}
}

func TestVectorBackupRestoreReIndexing(t *testing.T) {
	conf := dgraphtest.NewClusterConfig().WithNumAlphas(1).WithNumZeros(1).WithReplicas(1).WithACL(time.Hour)
	c, err := dgraphtest.NewLocalCluster(conf)
	require.NoError(t, err)
	defer func() { c.Cleanup(t.Failed()) }()
	require.NoError(t, c.Start())

	gc, cleanup, err := c.Client()
	require.NoError(t, err)
	defer cleanup()
	require.NoError(t, gc.LoginIntoNamespace(context.Background(),
		dgraphapi.DefaultUser, dgraphapi.DefaultPassword, x.RootNamespace))

	hc, err := c.HTTPClient()
	require.NoError(t, err)
	require.NoError(t, hc.LoginIntoNamespace(dgraphapi.DefaultUser,
		dgraphapi.DefaultPassword, x.RootNamespace))

	require.NoError(t, gc.SetupSchema(testSchema))

	numVectors := 1000
	pred := "project_description_v"
	rdfs, vectors := dgraphapi.GenerateRandomVectors(0, numVectors, 10, pred)

	mu := &api.Mutation{SetNquads: []byte(rdfs), CommitNow: true}
	_, err = gc.Mutate(mu)
	require.NoError(t, err)

	t.Log("taking backup \n")
	require.NoError(t, hc.Backup(c, false, dgraphtest.DefaultBackupDir))

	rdfs2, vectors2 := dgraphapi.GenerateRandomVectors(numVectors, numVectors+300, 10, pred)

	mu = &api.Mutation{SetNquads: []byte(rdfs2), CommitNow: true}
	_, err = gc.Mutate(mu)
	require.NoError(t, err)
	t.Log("restoring backup \n")
	require.NoError(t, hc.Restore(c, dgraphtest.DefaultBackupDir, "", 2, 1))
	require.NoError(t, dgraphapi.WaitForRestore(c))

	for i := 0; i < 5; i++ {
		// drop index
		require.NoError(t, gc.SetupSchema(testSchemaWithoutIndex))
		// add index
		require.NoError(t, gc.SetupSchema(testSchema))
	}
	vectors = append(vectors, vectors2...)
	rdfs = rdfs + rdfs2
	testVectorQuery(t, gc, vectors, rdfs, pred, numVectors)
}
